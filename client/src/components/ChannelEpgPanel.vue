<script setup lang="ts">
import { client, restClient } from '@/libs/client'
import { useChannelsStores } from '@/stores/channels'
import { useNodesStore } from '@/stores/nodes'
import { get } from 'lodash'
import { computed, onMounted, ref } from 'vue'
const playoutChannels = ref<any[]>([])
const channelAvaialbelAssigned = ref<any[]>([])
const nodeStore = useNodesStore()
const nodes = computed(() => nodeStore.data)
const nodeIds = computed(() =>
  nodes.value
    .filter((i) => i.status !== 'ended')
    .map((i) => {
      let uuid = i.uuid?.match(/([a-z]+)-([a-z]+)-(.*)/)?.at(-1)
      if (uuid) return uuid
      return i.uuid
    })
)

const channelStore = useChannelsStores()

const fetchData = async () => {
  const data = get(
    await restClient.get(
      '/accounts/channels?$eager=[channelEpg]&$limit=100&$sort[channel.date_created]=-1&is_virtual=false&is_playout_assigned=true',
      {
        headers: {
          Authorization: localStorage.getItem('token') || ''
        }
      }
    ),
    'data.data'
  )

  const data2 = get(
    await restClient.get(
      '/accounts/channels?$eager=[channelEpg]&$limit=100&$sort[channel.date_created]=-1&is_virtual=false&is_playout_assigned=false&$withoutNoPodAssign',
      {
        headers: {
          Authorization: localStorage.getItem('token') || ''
        }
      }
    ),
    'data.data'
  )

  const accounts = playoutChannels.value.reduce((acc: any, item: any) => {
    if (item.account && !acc[item.account?.id]) {
      acc[item.account?.id] = item.account
    }
    return acc
  }, {})

  playoutChannels.value = data
  channelAvaialbelAssigned.value = data2

  channelStore.setValues(playoutChannels.value)
  accounts.value = Object.values(accounts)
}
onMounted(async () => {
  fetchData()
})

const sortedData = computed(() => {
  let beforeItems = []
  let afterItems = []

  for (const item of playoutChannels.value) {
    if (checkChannelLive(item.id)) {
      beforeItems.push({
        ...item,
        isLive: true
      })
    } else {
      afterItems.push(item)
    }
  }
  return beforeItems.concat(afterItems)
})

const onClick = async (item: any) => {
  loading.value = true
  try {
    await client.get(`/start-live-channels?id=${get(item, 'id')}`)
  } finally {
    loading.value = false
  }
}

const loading = ref(false)

const checkChannelLive = (channelId: string) => nodeIds.value.some((i) => i === channelId)
const openAddChannel = ref(false)
const selected = ref<any>(null)
const openAddChannelFn = () => (openAddChannel.value = true)

const addChannel = async () => {
  loading.value = true
  openAddChannel.value = false
  try {
    await restClient.patch(
      '/accounts/channels/' + selected.value,
      {
        is_playout_assigned: true
      },
      {
        headers: {
          Authorization: localStorage.getItem('token') || ''
        }
      }
    )
  } finally {
    loading.value = false
    selected.value = null
    fetchData()
  }
}

const removeChannel = async (channelId: string) => {
  loading.value = true
  try {
    await restClient.patch(
      '/accounts/channels/' + channelId,
      {
        is_playout_assigned: false
      },
      {
        headers: {
          Authorization: localStorage.getItem('token') || ''
        }
      }
    )
  } finally {
    loading.value = false
    fetchData()
  }
}
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
        <v-icon @click="openAddChannelFn">mdi-plus</v-icon>
      </p>
      <v-list max-height="300">
        <v-list-item
          v-for="item in sortedData"
          :key="item.id"
          :title="item.name"
          class="ma-3 bg-gray-200 cursor-pointer hover:bg-gray-600 hover:text-white"
          :disabled="item.isLive"
          :class="{ 'bg-red-500': item.isLive }"
        >
          <template #prepend>
            <img class="h-[50px] w-[70px] ml-2" :src="item.img_url" alt="" />
          </template>
          <template #title>
            <span>
              {{ item.name }}
            </span>
          </template>
          <template #append>
            <div class="flex flex-row gap-2">
              <v-icon
                class="hover:cursor-pointer hover:text-red"
                @click="onClick(item)"
                v-if="!item.isLive"
                >mdi-play</v-icon
              >
              <v-icon
                class="hover:cursor-pointer hover:text-red"
                @click="removeChannel(item.id)"
                v-if="!item.isLive"
                >mdi-minus</v-icon
              >
            </div>
          </template>
        </v-list-item>
      </v-list>

      <v-dialog v-model:model-value="openAddChannel" max-width="600px">
        <v-card>
          <v-card-title>Add Channel</v-card-title>
          <v-card-text>
            <v-select
              v-model="selected"
              :items="channelAvaialbelAssigned"
              item-title="name"
              item-value="id"
            >
            </v-select>
          </v-card-text>
          <v-card-actions>
            <v-btn @click="openAddChannel = false">Cancel</v-btn>
            <v-btn @click="addChannel">Save</v-btn>
          </v-card-actions>
        </v-card>
      </v-dialog>
    </div>
  </div>
</template>
