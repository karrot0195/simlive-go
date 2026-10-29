<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { api } from '@/libs/api'
import { segmentUrl } from '@/libs/hls'
import type { ChannelTimeline, TimelineEntry, PreparedSegmentsResp } from '@/types/api'

const loading = ref(false)
const error = ref<string | null>(null)
const channels = ref<ChannelTimeline[]>([])
const prepared = ref<Record<string, PreparedSegmentsResp>>({})
const generatedAt = ref<number>(0)
const now = ref<number>(Date.now() / 1000)
const intervalMs = ref<number>(Number(localStorage.getItem('timeline_interval_ms') || 2000))
let timer: ReturnType<typeof setInterval> | null = null

const restartTimer = () => {
  if (timer) clearInterval(timer)
  timer = setInterval(() => {
    now.value = Date.now() / 1000
    channels.value = channels.value.map((ch) => ({
      ...ch,
      entries: ch.entries.filter((e) => e.running_time + e.duration > now.value)
    }))
    fetchPrepared()
  }, intervalMs.value)
}

watch(intervalMs, (v) => {
  localStorage.setItem('timeline_interval_ms', String(v))
  restartTimer()
})

const load = async () => {
  loading.value = true
  error.value = null
  try {
    const resp = await api.getRunningChannelsTimeline()
    channels.value = resp.channels ?? []
    generatedAt.value = resp.generated_at
    await fetchPrepared()
  } catch (e: any) {
    error.value = e?.response?.data?.error ?? e?.message ?? 'Failed to load timeline'
  } finally {
    loading.value = false
  }
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
  const ids = channels.value.map((c) => c.channel_id)
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

// prune fully-passed segments reactively
const visibleChannels = computed(() => {
  return channels.value.map((ch) => ({
    ...ch,
    entries: ch.entries.filter((e) => e.running_time + e.duration > now.value)
  }))
})

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
  restartTimer()
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <v-container fluid>
    <v-row class="mb-2">
      <v-col>
        <h2>Running Channels Timeline</h2>
      </v-col>
      <v-col cols="auto">
        <v-text-field
          v-model.number="intervalMs"
          label="Interval (ms)"
          type="number"
          density="compact"
          hide-details
          style="width: 120px"
        />
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
      <v-card-title>{{ ch.channel_name }} ({{ ch.channel_id }})</v-card-title>
      <v-card-subtitle>{{ channelSummary(ch) }}</v-card-subtitle>
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
  </v-container>
</template>

<style scoped>
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