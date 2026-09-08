<script setup lang="ts">
import { ref } from 'vue'
import ChannelEpgPanel from '@/components/ChannelEpgPanel.vue'
import SimulatedLive from '@/components/SimulatedLive.vue'

const simulatedLiveRef = ref<InstanceType<typeof SimulatedLive> | null>(null)

const onSwitch = (id: string) => {
  simulatedLiveRef.value?.setActive(id)
}
</script>

<template>
  <SimulatedLive ref="simulatedLiveRef">
    <template #tools>
      <ChannelEpgPanel @switch="onSwitch" />
    </template>
  </SimulatedLive>
</template>
