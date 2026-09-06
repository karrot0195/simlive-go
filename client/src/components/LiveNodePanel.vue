<template>
  <div>
    <p class="text-h6">List Node Live</p>
    <v-list
      class="flex flex-col gap-2 h-[400px] rounded-sm border-dashed border-md border-secondary w-100 overflow-y-scroll"
    >
      <v-list-item
        v-for="(node, i) in listNodes"
        :key="i"
        class="p-2 bg-gray-50 hover:bg-blue-400 cursor-pointer rounded-sm text-sm flex gap-2 align-center justify-between h-[100px]"
        :class="{
          'font-extrabold text-xl  bg-gray-500 text-white': store.activeNodeId === node.uuid,
          'text-blue-500 bg-blue-200': actionEffect === node.uuid,
          flicker: store.activeNodeId === node.uuid && node.status === 'ready'
        }"
      >
        <template #prepend>
          <div class="p-1 text-red-500" v-if="node.status === 'ready'">
            <v-icon>mdi-stop</v-icon>
            <span>LIVE</span>
          </div>
          <div class="p-1 text-gray-400 opacity-60" v-else-if="node.status === 'ended'">
            <v-icon>mdi-stop</v-icon>
            ENDED
          </div>
          <div class="p-1 text-blue-400" v-else-if="node.status === 'inprocess'">
            <v-progress-circular></v-progress-circular>
            PROCESSING
          </div>
          <div class="p-1 text-gray-400 uppercase" v-else>
            {{ node.status }}
          </div>
        </template>
        <template #title>
          <div class="text-center">
            <p class="text-bold">{{ node.name ?? node.uuid }}</p>
            <p v-if="node?.time">passed {{ node?.time }}</p>
            <p v-if="node?.metadata?.time">passed {{ parseInt(node?.metadata?.time) }} s</p>
            <p v-if="node?.metadata?.timeAlive">
              alive {{ (+new Date() - node?.metadata?.timeAlive) / 1000 }} secs ago
            </p>
          </div>
        </template>
        <template #append>
          <div class="flex gap-1 items-end justify-end">
            <v-tooltip text="watch live">
              <template #activator="{ props }">
                <v-icon
                  class="hover:text-red-500"
                  v-if="store.activeNodeId !== node.uuid && node.status === 'ready'"
                  @click="handleClick(node.uuid)"
                  v-bind="props"
                  >mdi-play</v-icon
                >
              </template>
            </v-tooltip>

            <v-tooltip text="View logs">
              <template #activator="{ props }">
                <v-icon class="hover:text-red-500" @click="navigateToLogs(node)" v-bind="props"
                  >mdi-file-document-outline</v-icon
                >
              </template>
            </v-tooltip>

            <v-tooltip text="Copy link">
              <template #activator="{ props }">
                <v-icon v-bind="props" @click="copyNodeLink(node)" class="hover:text-red-500"
                  >mdi-content-copy</v-icon
                >
              </template>
            </v-tooltip>

            <v-tooltip text="restart" v-if="node.status !== 'stop'">
              <template #activator="{ props }">
                <v-icon v-bind="props" @click="restartNode(node)" class="hover:text-red-500"
                  >mdi-restart</v-icon
                >
              </template>
            </v-tooltip>

            <v-tooltip text="delete">
              <template #activator="{ props }">
                <v-icon v-bind="props" @click="deleteNode(node)" class="hover:text-red-500"
                  >mdi-delete</v-icon
                >
              </template>
            </v-tooltip>
          </div>
        </template>
      </v-list-item>
    </v-list>
  </div>
</template>
<script setup lang="ts">
import { API, LIVE_API } from '@/constants'
import { client } from '@/libs/client'
import { copyText } from '@/libs/Helper'
import { useNodesStore } from '@/stores/nodes'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

defineProps({})

const route = useRoute()
const router = useRouter()

const listNodes = computed(() => store.data)

const store = useNodesStore()

const fetchLiveNodes = async () => {
  try {
    let data = ((await client.get('/node-lives'))?.data?.data ?? []).reverse()
    data = data.map((item: any) => {
      return item
    })

    store.setValues(data)

    // Auto-select first ready node if no active node
    if (!store.activeNodeId && data.length > 0) {
      const firstReadyNode = data.find((node: any) => node.status === 'ready')
      if (firstReadyNode) {
        store.setactiveNodeId(firstReadyNode.uuid)
      } else if (data[0]) {
        // If no ready node, select the first node
        store.setactiveNodeId(data[0].uuid)
      }
    }
  } finally {
    /* empty */
  }
}

const handleClick = (nodeId: string) => {
  store.setactiveNodeId(null)
  setTimeout(() => {
    store.setactiveNodeId(nodeId)
    // Update query params
    router.push({
      query: {
        ...route.query,
        nodeId: nodeId
      }
    })
  }, 500)
}

const actionEffect = ref(null)
const copyNodeLink = (node: any) => {
  actionEffect.value = node.uuid
  const liveUrl = LIVE_API.match(/^(https?:\/\/[^\/]+)/) ? LIVE_API : window.location.origin
  copyText(`${liveUrl}/live/${node.uuid}/playout.m3u8`)

  setTimeout(() => {
    actionEffect.value = null
  }, 2000)
}

const restartNode = async (node: any) => {
  client.get(`/node-lives/${node.uuid}/reset`)
  await fetchLiveNodes()
}

const deleteNode = async (node: any) => {
  client.delete(`/node-lives/${node.uuid}`)
  await fetchLiveNodes()
}

const navigateToLogs = (node: any) => {
  const queryParams = new URLSearchParams({
    folder: 'logs',
    viewFile: 'worker.log',
    search: node.uuid
  })
  window.open(`/tmp-data?${queryParams.toString()}`, '_blank')
}

// Watch for query param changes
watch(
  () => route.query.nodeId,
  (nodeId) => {
    if (nodeId && typeof nodeId === 'string') {
      store.setactiveNodeId(nodeId)
    }
  },
  { immediate: true }
)

// Watch for active node changes and update query params
watch(
  () => store.activeNodeId,
  (nodeId) => {
    if (nodeId && route.query.nodeId !== nodeId) {
      router.replace({
        query: {
          ...route.query,
          nodeId: nodeId
        }
      })
    }
  }
)

onMounted(async () => {
  await fetchLiveNodes()

  // If nodeId in query params, use it
  if (route.query.nodeId && typeof route.query.nodeId === 'string') {
    store.setactiveNodeId(route.query.nodeId)
  }

  const token = localStorage.getItem('token')
  const eventSource = new EventSource(`${API}/sse/node-live?token=${token}`, {
    withCredentials: true
  })
  eventSource.onmessage = ({ data }) => {
    console.log('SSE received:', JSON.parse(data))
    const parsedData = JSON.parse(data)
    if (parsedData && Array.isArray(parsedData)) {
      store.setValues(parsedData.reverse())
    }
  }
  eventSource.onerror = (error) => {
    console.error('SSE error:', error)
  }
})
</script>

<style scoped>
@media (max-width: 768px) {
  .h-\[400px\] {
    height: 300px !important;
  }

  .h-\[100px\] {
    height: auto !important;
    min-height: 80px !important;
  }

  .text-sm {
    font-size: 0.75rem !important;
  }

  .text-h6 {
    font-size: 1rem !important;
  }

  .p-2 {
    padding: 0.5rem !important;
  }

  .gap-2 {
    gap: 0.25rem !important;
  }

  .gap-1 {
    gap: 0.125rem !important;
  }
}

@media (max-width: 480px) {
  .h-\[400px\] {
    height: 250px !important;
  }

  .text-sm {
    font-size: 0.7rem !important;
  }

  .v-icon {
    font-size: 1rem !important;
  }
}
</style>
