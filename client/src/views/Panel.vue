<script setup lang="ts">
import ChannelEpgPanel from '@/components/ChannelEpgPanel.vue'
import SimulatedLive from '@/components/SimulatedLive.vue'
</script>

<template>
  <SimulatedLive>
    <template #tools>
      <ChannelEpgPanel />
    </template>
  </SimulatedLive>
</template>
