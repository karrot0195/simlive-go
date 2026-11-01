<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { api } from '@/libs/api'
import { segmentUrl } from '@/libs/hls'
import type { ChannelTimeline, TimelineEntry, PreparedSegmentsResp, SegmentTrackEntry } from '@/types/api'

const loading = ref(false)
const error = ref<string | null>(null)
const channels = ref<ChannelTimeline[]>([])
const prepared = ref<Record<string, PreparedSegmentsResp>>({})
const generatedAt = ref<number>(0)
const now = ref<number>(Date.now() / 1000)
let eventSource: EventSource | null = null

const load = async () => {
  loading.value = true
  error.value = null
  try {
    const resp = await api.getRunningChannelsTimeline()
    channels.value = resp.channels ?? []
    generatedAt.value = resp.generated_at
    const monitoredResp = await api.listMonitoredChannels().catch(() => ({ channel_ids: [] }))
    monitored.value = new Set(monitoredResp.channel_ids || [])
    await fetchPrepared()
  } catch (e: any) {
    error.value = e?.response?.data?.error ?? e?.message ?? 'Failed to load timeline'
  } finally {
    loading.value = false
  }
}

const connectSSE = () => {
  if (eventSource) eventSource.close()
  eventSource = new EventSource('/api/channels/timeline/stream')
  eventSource.onmessage = async (ev) => {
    try {
      const data = JSON.parse(ev.data)
      if (Array.isArray(data.channels)) {
        // Update watched channels in place; channels that were un-watched
        // (omitted from the payload) are left untouched so they remain
        // visible but stop receiving segment updates.
        const next = [...channels.value]
        for (const ch of data.channels) {
          const idx = next.findIndex((c) => c.channel_id === ch.channel_id)
          if (idx >= 0) next[idx] = ch
          else next.push(ch)
        }
        channels.value = next
      }
      if (data.generated_at) generatedAt.value = data.generated_at
      now.value = Date.now() / 1000
      await fetchPrepared()
    } catch (e) {
      // ignore parse errors
    }
  }
  eventSource.onerror = () => {
    // browser will auto-reconnect
  }
}

const monitored = ref<Set<string>>(new Set())

const toggleWatch = async (id: string) => {
  if (monitored.value.has(id)) {
    await api.unwatchChannel(id)
    monitored.value.delete(id)
  } else {
    await api.watchChannel(id)
    monitored.value.add(id)
  }
  monitored.value = new Set(monitored.value)
}

const preparedRenditions = (ch: ChannelTimeline, e: TimelineEntry) => {
  const p = prepared.value[ch.channel_id]
  if (!p) return []
  const name = `seg_${e.sequence}.ts`
  return Object.entries(p.renditions || {})
    .filter(([, list]) => (list || []).includes(name))
    .map(([rend]) => rend)
}

const isPrepared = (ch: ChannelTimeline, e: TimelineEntry) => preparedRenditions(ch, e).length > 0

const isRunning = (e: TimelineEntry) =>
  e.running_time <= now.value && now.value < e.running_time + e.duration

const gapSeconds = (a: TimelineEntry, b: TimelineEntry) =>
  b.running_time - (a.running_time + a.duration)

const fetchPrepared = async () => {
  const ids = channels.value.filter((c) => monitored.value.has(c.channel_id)).map((c) => c.channel_id)
  const results = await Promise.all(
    ids.map((id) =>
      api.getPreparedSegments(id).catch(() => null)
    )
  )
  const next: Record<string, PreparedSegmentsResp> = { ...prepared.value }
  results.forEach((r, i) => {
    if (r) next[ids[i]] = r
  })
  prepared.value = next
}

const fmtTime = (ts: number) => {
  const d = new Date(ts * 1000)
  return d.toLocaleTimeString()
}

const selectedSegment = ref<TimelineEntry | null>(null)
const selectedChannel = ref<string>('')
const dialogOpen = ref(false)

const trackDialogOpen = ref(false)
const trackChannel = ref<string>('')
const trackEntries = ref<SegmentTrackEntry[]>([])
const trackLoading = ref(false)
const videoSrc = computed(() => {
  if (!selectedSegment.value) return ''
  return segmentUrl(selectedChannel.value, '360p', `seg_${selectedSegment.value.sequence}.ts`)
})
const playSegment = (ch: ChannelTimeline, e: TimelineEntry) => {
  selectedChannel.value = ch.channel_id
  selectedSegment.value = e
  dialogOpen.value = true
}
const closePlayer = () => {
  dialogOpen.value = false
  selectedSegment.value = null
}

const viewTrack = async (ch: ChannelTimeline) => {
  trackChannel.value = ch.channel_id
  trackEntries.value = []
  trackDialogOpen.value = true
  trackLoading.value = true
  try {
    const resp = await api.getSegmentTrack(ch.channel_id, 100)
    trackEntries.value = (resp.entries ?? []).slice().sort((a, b) => a.sequence - b.sequence)
  } catch (e) {
    trackEntries.value = []
  } finally {
    trackLoading.value = false
  }
}

const reloadTrack = async () => {
  if (!trackChannel.value) return
  trackLoading.value = true
  try {
    const resp = await api.getSegmentTrack(trackChannel.value, 100)
    trackEntries.value = (resp.entries ?? []).slice().sort((a, b) => a.sequence - b.sequence)
  } catch (e) {
    // ignore
  } finally {
    trackLoading.value = false
  }
}
const closeTrack = () => {
  trackDialogOpen.value = false
  trackEntries.value = []
}

const clearTrack = async () => {
  try {
    await api.clearSegmentTrack(trackChannel.value)
    trackEntries.value = []
  } catch (e) {
    // ignore
  }
}

const trackSummary = computed(() => {
  const entries = trackEntries.value
  if (!entries.length) return 'No track entries.'
  const seqs = entries.map((e) => e.sequence)
  const from = seqs[0]
  const to = seqs[seqs.length - 1]
  const gaps: string[] = []
  for (let i = 1; i < seqs.length; i++) {
    if (seqs[i] - seqs[i - 1] > 1) {
      gaps.push(`#${seqs[i - 1]}→#${seqs[i]} (missing ${seqs[i] - seqs[i - 1] - 1})`)
    }
  }
  const base = `${entries.length} entries, seq #${from}–#${to}.`
  if (!gaps.length) return base + ' No gaps.'
  return base + ' Gaps: ' + gaps.join('; ') + '.'
})
// prune fully-passed segments reactively
const visibleChannels = computed(() => {
  return channels.value.map((ch) => ({
    ...ch,
    entries: ch.entries.filter((e) => e.running_time + e.duration > now.value)
  }))
})

const loadedAhead = (ch: ChannelTimeline) =>
  ch.entries.filter((e) => e.running_time + e.duration > now.value && isPrepared(ch, e)).length

const channelSummary = (ch: ChannelTimeline) => {
  const entries = ch.entries.filter((e) => e.running_time + e.duration > now.value)
  if (!entries.length) return 'No upcoming entries.'
  const first = entries[0]
  const last = entries[entries.length - 1]
  const from = fmtTime(first.running_time)
  const to = fmtTime(last.running_time + last.duration)
  const gaps: string[] = []
  for (let i = 1; i < entries.length; i++) {
    const g = gapSeconds(entries[i - 1], entries[i])
    if (g > 10) {
      gaps.push(`${fmtTime(entries[i - 1].running_time + entries[i - 1].duration)} → ${fmtTime(entries[i].running_time)} (${g.toFixed(1)}s)`)
    }
  }
  if (!gaps.length) return `Data fills from ${from} to ${to}. No warning gaps.`
  return `Data fills from ${from} to ${to}. Warning gaps: ${gaps.join('; ')}.`
}

onMounted(() => {
  load()
  connectSSE()
})

onUnmounted(() => {
  if (eventSource) eventSource.close()
})
</script>

<template>
  <v-container fluid>
    <v-row class="mb-2">
      <v-col>
        <h2>Running Channels Timeline</h2>
      </v-col>
      <v-col cols="auto">
        <span class="text-medium-emphasis">Updates via SSE</span>
      </v-col>
      <v-col cols="auto">
        <v-btn color="primary" :loading="loading" @click="load">Refresh</v-btn>
      </v-col>
    </v-row>

    <v-alert v-if="error" type="error" class="mb-2">
      {{ error }}
      <template #append>
        <v-btn size="small" @click="load">Retry</v-btn>
      </template>
    </v-alert>

    <div v-if="!loading && !visibleChannels.length" class="text-medium-emphasis">
      No running channels or upcoming entries.
    </div>

    <v-card v-for="ch in visibleChannels" :key="ch.channel_id" class="mb-4">
      <v-card-title>
  {{ ch.channel_name }} ({{ ch.channel_id }})
  <span class="fallback-slot">
    <v-chip v-if="ch.fallback_active" color="warning" size="small" class="ml-2" variant="flat">
      FALLBACK: {{ ch.fallback_source_date || 'unknown' }}
    </v-chip>
  </span>
  <v-btn size="small" class="ml-2" @click="toggleWatch(ch.channel_id)">
    {{ monitored.has(ch.channel_id) ? 'Unwatch' : 'Watch' }}
  </v-btn>
  <v-btn size="small" class="ml-2" variant="outlined" @click="viewTrack(ch)">View track</v-btn>
</v-card-title>
      <v-card-subtitle>{{ channelSummary(ch) }} • Loaded ahead: {{ loadedAhead(ch) }}</v-card-subtitle>
      <v-card-text>
        <div v-if="!ch.entries.length" class="text-medium-emphasis">No upcoming entries.</div>
        <div v-else class="timeline-scroll">
          <div class="timeline-row">
            <div
              v-for="(e, i) in ch.entries"
              :key="e.sequence"
              class="entry"
              :class="{ ad: e.is_ad, gap: i > 0 && gapSeconds(ch.entries[i - 1], e) > 10, running: isRunning(e) }"
              @click="isPrepared(ch, e) ? playSegment(ch, e) : null"
              :style="{ cursor: isPrepared(ch, e) ? 'pointer' : 'default', opacity: isPrepared(ch, e) ? 1 : 0.5 }"
            >
              <span class="time">{{ e.time_text }}</span>
              <span class="seq">#{{ e.sequence }}</span>
              <span class="dur">{{ e.duration.toFixed(1) }}s</span>
              <span v-if="e.is_ad" class="badge">AD</span>
              <span v-else class="vod">VOD</span>
              <span v-for="r in preparedRenditions(ch, e)" :key="r" class="ready">{{ r }}</span>
              <span v-if="!isPrepared(ch, e)" class="notready">NOT READY</span>
              <span
                v-if="i > 0 && gapSeconds(ch.entries[i - 1], e) > 10"
                class="warn"
              >
                Gap {{ gapSeconds(ch.entries[i - 1], e).toFixed(1) }}s
              </span>
            </div>
          </div>
        </div>
      </v-card-text>
    </v-card>
    <v-dialog v-model="dialogOpen" max-width="640" @update:model-value="v => { if (!v) closePlayer() }">
      <v-card>
        <v-card-title>Play segment #{{ selectedSegment?.sequence }} (360p)</v-card-title>
        <v-card-text>
          <video :src="videoSrc" controls autoplay style="width: 100%"></video>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="closePlayer">Close</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="trackDialogOpen" max-width="720" @update:model-value="v => { if (!v) closeTrack() }">
      <v-card min-height="200">
        <v-card-title>Segment track — {{ trackChannel }}</v-card-title>
        <v-card-subtitle class="text-medium-emphasis">Audit of prepared segments; may differ from the current timeline window.</v-card-subtitle>
        <v-card-text class="text-medium-emphasis pt-0">{{ trackSummary }}</v-card-text>
        <v-card-text>
          <div v-if="trackLoading" class="text-medium-emphasis">Loading...</div>
          <div v-else-if="!trackEntries.length" class="text-medium-emphasis">No track entries.</div>
          <v-table v-else density="compact">
            <thead>
              <tr><th>Seq</th><th>Type</th><th>Renditions</th><th>Time</th><th>Message</th></tr>
            </thead>
            <tbody>
              <tr v-for="(e, i) in trackEntries" :key="i">
                <td>#{{ e.sequence }}</td>
                <td>{{ e.is_ad ? 'AD' : 'VOD' }}</td>
                <td>{{ (e.renditions || []).join(', ') || '-' }}</td>
                <td>{{ new Date(e.timestamp * 1000).toLocaleString() }}</td>
                <td>{{ e.message || '-' }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
        <v-card-actions>
          <v-btn color="error" variant="text" @click="clearTrack">Clear</v-btn>
          <v-btn variant="text" @click="reloadTrack" :loading="trackLoading">Reload</v-btn>
          <v-spacer />
          <v-btn @click="closeTrack">Close</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>
<style scoped>
.fallback-slot {
  display: inline-flex;
  align-items: center;
  min-width: 190px;
  min-height: 24px;
  vertical-align: middle;
}
.timeline-scroll {
  overflow-x: auto;
  padding-bottom: 8px;
  min-height: 80px;
}
.timeline-row {
  display: flex;
  flex-direction: row;
  gap: 8px;
  min-width: max-content;
  align-items: stretch;
}
.entry {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 8px 12px;
  border-radius: 6px;
  background: #f5f5f5;
  border: 1px solid #e0e0e0;
  min-width: 90px;
  min-height: 72px;
  flex: 0 0 auto;
}
.entry.ad {
  background: #fff3e0;
}
.entry.running {
  background: #c8e6c9;
  border: 2px solid #2e7d32;
}
.ready {
  background: #2e7d32;
  color: #fff;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 12px;
}
.notready {
  background: #9e9e9e;
  color: #fff;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 12px;
}
.badge {
  background: #fbc02d;
  color: #333;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 12px;
}
.badge, .vod, .ready, .notready {
  min-height: 18px;
  display: inline-flex;
  align-items: center;
  line-height: 1;
}
.vod {
  background: #1976d2;
  color: #fff;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 12px;
}
</style>