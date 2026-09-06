<script setup lang="ts">
import { computed } from 'vue'
import 'video.js/dist/video-js.css'
import 'video.js'
import LiveNodePanel from './LiveNodePanel.vue'
import LiveStream from '@/components/LiveStream.vue'
import { useNodesStore } from '@/stores/nodes'
import { useAuthStore } from '@/stores/auth'
const isShowLive = computed(() => {
  return store.activeNodeId && activeNode && activeNode.value.status === 'ready'
})
const activeNode = computed(() => {
  return nodes.value.find((i) => i.uuid === store.activeNodeId)
})
const store = useNodesStore()
const nodes = computed(() => store.data)
const { data, reset } = useAuthStore()
</script>

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
      <div class="">
        <LiveNodePanel></LiveNodePanel>
      </div>
    </div>
    <div
      class="flex flex-col justify-center items-center grow rounded-md border-primary border-md"
      style="background-color: rgb(255, 255, 255, 0.8); height: calc(100vh - 64px)"
    >
      <LiveStream
        v-if="isShowLive && activeNode"
        :node="activeNode"
        type="application/vnd.apple.mpegurl"
        class="w-100 h-100"
        style=""
      />
      <div v-else class="flex justify-center font-bold text-xl">
        <div v-if="activeNode?.status === 'inprocess'">
          <v-progress-circular color="primary" indeterminate></v-progress-circular>
        </div>
        <div v-else-if="activeNode?.status === 'ended'">
          <v-icon size="100" class="text-gray-200">mdi-stop-circle</v-icon>
        </div>
      </div>
    </div>
  </div>
</template>

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
