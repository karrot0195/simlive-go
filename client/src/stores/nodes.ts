import { ref } from 'vue'
import { defineStore } from 'pinia'

export const useNodesStore = defineStore('nodes', () => {
  const data = ref<any[]>([])
  const activeNodeId = ref<string | null>(null)
  function setValues(values: any[]) {
    data.value = values
  }

  function setactiveNodeId(nodeId: string | null) {
    activeNodeId.value = nodeId
  }
  return { data, setValues, activeNodeId, setactiveNodeId }
})
