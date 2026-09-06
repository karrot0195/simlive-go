<script setup lang="ts">
interface Props {
  content: string
}

defineProps<Props>()
</script>

<template>
  <v-card-text class="pa-0">
    <pre class="file-content">{{ content }}</pre>
  </v-card-text>
</template>

<style scoped>
.file-content {
  background-color: #f5f5f5;
  border: 1px solid #e0e0e0;
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
  font-family: 'Courier New', monospace;
  font-size: 14px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-wrap: break-word;
  max-height: 600px;
  overflow-y: auto;
}

@media (max-width: 600px) {
  .file-content {
    padding: 12px;
    font-size: 12px;
    max-height: 400px;
  }
}
</style>
