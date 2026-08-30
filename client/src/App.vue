<template>
  <main class="player-shell">
    <VideoPlayer />
  </main>
</template>

<script setup lang="ts">
import VideoPlayer from './components/VideoPlayer.vue'
</script>

<style scoped>
.player-shell {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: stretch;
  justify-content: stretch;
  padding: 0;
  margin: 0;
  background: black;
  color: white;
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
</style>