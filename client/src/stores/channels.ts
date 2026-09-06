import { ref } from 'vue'
import { defineStore } from 'pinia'

export const useChannelsStores = defineStore('channels', () => {
  const data = ref<any[]>([])
  function setValues(values: any[]) {
    data.value = values
  }

  return { data, setValues }
})
