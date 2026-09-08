import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/libs/api'
import type { ListChannelsParams, PlayoutChannel } from '@/types/api'

export const useChannelsStores = defineStore('channels', () => {
  const data = ref<PlayoutChannel[]>([])
  const loading = ref<boolean>(false)
  const error = ref<string | null>(null)

  function setValues(values: PlayoutChannel[]) {
    data.value = values
  }

  /** Refresh the local cache from the server. */
  async function fetchChannels(params?: ListChannelsParams) {
    loading.value = true
    error.value = null
    try {
      const resp = await api.listChannels(params)
      data.value = resp.data
      return resp
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      throw e
    } finally {
      loading.value = false
    }
  }

  /**
   * Register a channel with the local API. The response is wrapped
   * in `{ message, data: PlayoutChannel }`; merge the row in.
   */
  async function addChannel(channelId: string) {
    const result = await api.createOrUpsertChannel({ channel_id: channelId })
    const row = result.data
    const idx = data.value.findIndex((c) => c.id === row.id)
    if (idx >= 0) data.value[idx] = row
    else data.value.push(row)
    return row
  }

  async function startChannel(id: string) {
    const row = await api.startChannel(id)
    replaceRow(row)
    return row
  }

  async function stopChannel(id: string) {
    const row = await api.stopChannel(id)
    replaceRow(row)
    return row
  }

  async function removeChannel(id: string) {
    await api.deleteChannel(id)
    data.value = data.value.filter((c) => c.id !== id)
  }

  async function refreshChannel(id: string) {
    const row = await api.getChannel(id)
    replaceRow(row)
    return row
  }

  function replaceRow(row: PlayoutChannel) {
    const idx = data.value.findIndex((c) => c.id === row.id)
    if (idx >= 0) data.value[idx] = row
    else data.value.push(row)
  }

  return {
    data,
    loading,
    error,
    setValues,
    fetchChannels,
    addChannel,
    startChannel,
    stopChannel,
    removeChannel,
    refreshChannel
  }
})
