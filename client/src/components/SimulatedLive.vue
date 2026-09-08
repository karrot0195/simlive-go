<template>
  <div class="flex flex-row gap-3 p-1">
    <div class="w-[400px] max-w-1/3">
      <div class="flex flex-row gap-3 items-center py-4">
        <div class="font-semibold text-xl">App: {{ data?.account?.name ?? 'No account' }}</div>
        <v-icon class="text-md hover:text-red-500 hover:cursor-pointer" size="small" @click="reset">
          mdi-logout</v-icon
        >
      </div>
      <slot name="tools"></slot>
    </div>
    <div
      class="flex flex-col justify-center items-center grow rounded-md border-primary border-md"
      style="background-color: rgb(255, 255, 255, 0.8); height: calc(100vh - 64px)"
    >
      <PlayerLiveStream
        v-if="isShowLive && activeChannel"
        :key="activeChannel?.id"
        :node="{ id: activeChannel.id, name: activeChannel.name, status: status }"
        type="application/vnd.apple.mpegurl"
        class="w-100 h-100"
        style=""
      />
      <div v-else class="flex justify-center font-bold text-xl">
        <div v-if="isEnded">
          <v-icon size="100" class="text-gray-200">mdi-stop-circle</v-icon>
        </div>
        <div v-else>
          <v-progress-circular color="primary" indeterminate></v-progress-circular>
        </div>
      </div>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import 'video.js/dist/video-js.css'
import 'video.js'
import PlayerLiveStream from '@/components/PlayerLiveStream.vue'
import { useChannelsStores } from '@/stores/channels'
import { useAuthStore } from '@/stores/auth'
import { useRoute } from 'vue-router'
import type { PlayoutChannel } from '@/types/api'

// The right-hand HLS player used to be driven by the legacy
// `/node-lives` SSE feed, which has been removed (not in swagger.yaml,
// not implemented in the Go server). The active playback source is
// now the channels store: a channel is "playing" when its status is
// `LIVE` or `PREPARE`. Auto-pick the first such channel, falling back
// to the first active row.

const channelStore = useChannelsStores()
const channels = computed<PlayoutChannel[]>(() => channelStore.data)
const activeChannelId = ref<string | null>(null)

const activeChannel = computed<PlayoutChannel | null>(() => {
  const id = activeChannelId.value
  if (id) {
    const found = channels.value.find((c) => c.id === id)
    if (found) return found
  }
  const playing = channels.value.find(
    (c) => c.status === 'LIVE' || c.status === 'PREPARE'
  )
  return playing ?? null
})

const isShowLive = computed(() => !!activeChannel.value)
const status = computed(() => activeChannel.value?.status ?? null)
const isEnded = computed(() => status.value === 'INACTIVE' || status.value === 'ERROR')

const { data, reset } = useAuthStore()

const route = useRoute()

const refresh = async () => {
  try {
    await channelStore.fetchChannels({  page_size: 100 })
  } catch (e) {
    console.error('[SimulatedLive] refresh failed:', e)
  }
}

const syncFromQuery = () => {
  const ch = route.query.channel
  if (typeof ch === 'string' && ch) {
    activeChannelId.value = ch
  }
}

watch(() => route.query.channel, (newVal) => {
  if (typeof newVal === 'string' && newVal) {
    activeChannelId.value = newVal
  }
}, { immediate: true })

onMounted(() => {
  syncFromQuery()
  void refresh()
  // Light polling so the player follows planner-driven status flips
  // (PREPARE -> LIVE -> INACTIVE) without depending on the legacy SSE.
  setInterval(refresh, 5_000)
})

const setActive = (id: string | null) => {
  activeChannelId.value = id
}

defineExpose({ setActive })
</script>

<style scoped>
@media (max-width: 1024px) {
  .flex-row {
    flex-direction: column !important;
  }

  .w-\[400px\] {
    width: 100% !important;
    max-width: 100% !important;
  }

  .p-1 {
    padding: 0.5rem !important;
  }

  div[style*="height: calc(100vh - 64px)"] {
    height: auto !important;
    min-height: 60vh !important;
  }
}

@media (max-width: 768px) {
  .gap-3 {
    gap: 0.75rem !important;
  }

  .py-4 {
    padding-top: 0.5rem !important;
    padding-bottom: 0.5rem !important;
  }

  .text-xl {
    font-size: 1rem !important;
  }

  .text-h6 {
    font-size: 1rem !important;
  }
}
</style>
