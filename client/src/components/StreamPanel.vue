<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import _, { map } from 'lodash'
import { client } from '../libs/client'
import { masterPlaylistUrl } from '@/libs/hls'
// @ts-ignore

const vodUrlList = ref<string[]>([])
const isProcessing = ref<boolean>(false)
const liveLink = ref<string | null>(null)
const hlsLink = ref<string | null>(null)
const startLive = async () => {
  isProcessing.value = true

  liveLink.value = null

  try {
    const data = await client.get(
      `/start_live?name=${campaignName.value}&m3u8Url=${vodUrlList.value.join(',')}`
    )
    liveLink.value = '/?nodeId=' + data.data.uuid
    hlsLink.value = masterPlaylistUrl(data.data.uuid)
  } catch (error) {
    console.error(error)
  } finally {
    setTimeout(() => {
      isProcessing.value = false
      campaignName.value = gencampaignName()
    }, 3000)
  }
}

const liveTextBtn = computed(() => {
  if (isProcessing.value) {
    return 'Processing'
  }
  return 'Start live'
})

const vodUrlSuggestList = ref<string[]>([])
onMounted(async () => {
  vodUrlSuggestList.value = [
    'wJKnkrGxcI9LDPhD8tJtT4mU',
    'LdTPlxtBKNwxvr82aTfihu1r',
    'y8zZdwEg8Tn2geGOnkMPDG1R'
  ].map((i: any) => `${i}/vod.m3u8`)
})

const gencampaignName = () =>
  `Live ${new Date().getHours()}:${new Date().getMinutes()} ${new Date().toDateString()}`
const campaignName = ref(gencampaignName())
</script>

<template>
  <div class="mt-2">
    <v-text-field class="text-black" label="Title" v-model="campaignName" />
    <p class="text-h6">List stream</p>
    <div
      class="px-1 mb-1 border-secondary border-md border-dashed overflow-scroll"
      style="min-height: 300px !important"
    >
      <v-list class="gap-1 flex flex-col bg-transparent">
        <v-list-item
          v-for="(streamUrl, i) in vodUrlList"
          :key="i"
          :title="streamUrl"
          class="hover:text-white cursor-pointer text-xs"
        >
          <template #append>
            <v-icon @click="vodUrlList.splice(i, 1)" class="cursor-pointer">mdi-close</v-icon>
          </template>
        </v-list-item>
      </v-list>
    </div>

    <div class="pt-1">
      <button :disabled="isProcessing" @click="startLive" class="p-2 bg-blue-300 hover:bg-blue-500">
        {{ liveTextBtn }}
      </button>
    </div>

    <div class="">
      <div class="text-h6">Suggest list</div>
      <div class="flex flex-col gap-2">
        <v-list class="gap-1 flex flex-col bg-transparent">
          <v-list-item
            @click="vodUrlList.push(vodUrl)"
            v-for="(vodUrl, i) of vodUrlSuggestList"
            :key="i"
            :title="vodUrl"
            class="cursor-pointer rounded-sm opacity-50 hover:font-bold bg-gray-400 hover:opacity-100 hover:text-white text-xs"
          >
          </v-list-item>
        </v-list>
      </div>
    </div>
  </div>
</template>

<style scoped>
@media (max-width: 768px) {
  .text-h6 {
    font-size: 1rem !important;
  }
  
  .text-xs {
    font-size: 0.65rem !important;
  }
  
  .p-2 {
    padding: 0.5rem !important;
  }
  
  .pt-1 {
    padding-top: 0.5rem !important;
  }
  
  .mt-2 {
    margin-top: 0.5rem !important;
  }
  
  .gap-2 {
    gap: 0.5rem !important;
  }
  
  .gap-1 {
    gap: 0.25rem !important;
  }
  
  button {
    width: 100% !important;
    font-size: 0.875rem !important;
  }
}

@media (max-width: 480px) {
  .px-1 {
    padding-left: 0.25rem !important;
    padding-right: 0.25rem !important;
  }
  
  .mb-1 {
    margin-bottom: 0.25rem !important;
  }
}
</style>
