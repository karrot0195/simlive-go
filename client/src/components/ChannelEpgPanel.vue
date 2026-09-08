<script setup lang="ts">
import { api } from '@/libs/api'
import { masterPlaylistUrl } from '@/libs/hls'
import { useChannelsStores } from '@/stores/channels'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { ESPXChannelListItem, PlayoutChannel, PlayoutChannelStatus } from '@/types/api'

type ChannelWithLive = PlayoutChannel & { isLive?: boolean }

const channelStore = useChannelsStores()
const playoutChannels = computed<PlayoutChannel[]>(() => channelStore.data)
const channelAvaialbelAssigned = ref<ESPXChannelListItem[]>([])
const loading = ref(false)

const isLiveStatus = (status: PlayoutChannelStatus | undefined) =>
  status === 'LIVE' || status === 'PREPARE'

const fetchData = async () => {
  loading.value = true
  try {
    const [active, esp] = await Promise.all([
      api.listChannels({ page_size: 100 }),
      api.listESpxChannels()
    ])
    channelStore.setValues(active.data)

    const registeredSet = new Set(
      Object.entries(esp.registered)
        .filter(([, v]) => v)
        .map(([id]) => id)
    )
    channelAvaialbelAssigned.value = esp.data.filter((c) => !registeredSet.has(c.id))
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await fetchData()
})

const sortedData = computed<ChannelWithLive[]>(() => {
  const beforeItems: ChannelWithLive[] = []
  const afterItems: ChannelWithLive[] = []

  for (const item of playoutChannels.value) {
    if (isLiveStatus(item.status)) {
      beforeItems.push({ ...item, isLive: true })
    } else {
      afterItems.push(item)
    }
  }
  return beforeItems.concat(afterItems)
})

const onClick = async (item: PlayoutChannel) => {
  loading.value = true
  try {
    await channelStore.startChannel(item.id)
    await router.push({ query: { ...route.query, channel: item.id } })
    emit('switch', item.id)
    await fetchData()
  } finally {
    loading.value = false
  }
}

const addChannel = async (channelId: string) => {
  try {
    await channelStore.addChannel(channelId)
    await fetchData()
  } finally {
    console.log("added channel")
  }
}

/** Hard-delete a stopped channel (soft-deletes via DELETE /api/channels/{id}). */
const deleteChannel = async (channelId: string) => {
  loading.value = true
  try {
    await channelStore.removeChannel(channelId)
    await fetchData()
  } finally {
    loading.value = false
  }
}

/** Stop a live channel (flip is_active=false, status=INACTIVE). */
const stopChannel = async (channelId: string) => {
  loading.value = true
  try {
    await channelStore.stopChannel(channelId)
    await fetchData()
  } finally {
    loading.value = false
  }
}

/** Restart: stop then immediately start a live channel. */
const restartChannel = async (channelId: string) => {
  loading.value = true
  try {
    await channelStore.stopChannel(channelId)
    await channelStore.startChannel(channelId)
    await fetchData()
  } finally {
    loading.value = false
  }
}

/** Copy the HLS master playlist URL to clipboard. */
const copyUrl = async (channelId: string) => {
  const url = masterPlaylistUrl(channelId)
  await navigator.clipboard.writeText(url)
}

const route = useRoute()
const router = useRouter()

const emit = defineEmits<{
  (e: 'switch', id: string): void
}>()
</script>

<template>
  <div class="relative">
    <v-overlay absolute v-model="loading" class="o-overplay">
      <div class="flex justify-center items-center w-screen h-screen">
        <v-progress-circular indeterminate :size="59"></v-progress-circular>
      </div>
    </v-overlay>
    <div>
      <p class="text-h6">
        List Playout Channel 
      </p>
      <v-list max-height="300">
        <v-list-item
          v-for="item in sortedData"
          :key="item.id"
          :title="item.name"
          class="ma-3 bg-gray-200 cursor-pointer hover:bg-gray-600 hover:text-white"
          :class="{ 'bg-red-500': route.query.channel === item.id }"
        >
          <template #title>
            <span>
              {{ item.name }}
              <span class="text-xs ml-2 opacity-70">({{ item.status }})</span>
              <v-icon
                size="x-small"
                class="ml-1 hover:cursor-pointer hover:text-blue"
                title="Copy HLS URL"
                @click.stop="copyUrl(item.id)"
                >mdi-content-copy</v-icon
              >
            </span>
          </template>
          <template #append>
            <div class="flex flex-row gap-2">
              <v-icon
                v-if="route.query.channel !== item.id"
                class="hover:cursor-pointer hover:text-blue"
                @click="onClick(item)"
                title="Select / Switch"
                >mdi-play-circle</v-icon
              >
              <!-- Live / PREPARE: show Stop + Restart -->
              <template v-if="item.isLive">
                <v-icon
                  class="hover:cursor-pointer hover:text-red"
                  @click="stopChannel(item.id)"
                  title="Stop"
                  >mdi-stop</v-icon
                >
                <v-icon
                  class="hover:cursor-pointer hover:text-blue"
                  @click="restartChannel(item.id)"
                  title="Restart"
                  >mdi-refresh</v-icon
                >
              </template>
              <!-- Inactive / Error: show Play + Delete -->
              <template v-else>
                <v-icon
                  class="hover:cursor-pointer hover:text-green"
                  @click="onClick(item)"
                  title="Start"
                  >mdi-play</v-icon
                >
                <v-icon
                  class="hover:cursor-pointer hover:text-red"
                  @click="deleteChannel(item.id)"
                  title="Delete"
                  >mdi-delete</v-icon
                >
              </template>
            </div>
          </template>
        </v-list-item>
      </v-list>
      <p>Channel Awaiting Assignment: ({{ channelAvaialbelAssigned.length }})</p>
      <v-list>
        <v-list-item
          v-for="(item) in channelAvaialbelAssigned"
          :key="item.id"
          class="ma-3 bg-gray-200  hover:bg-gray-600 hover:text-white"
        >
        <template #title>
          <div class="flex justify-between">
            {{ item.name }}
            <v-icon class="cursor-pointer" @click="addChannel(item.id)">mdi-plus</v-icon>
            </div>
        </template>
        </v-list-item>
      </v-list>
    </div>
  </div>
</template>
