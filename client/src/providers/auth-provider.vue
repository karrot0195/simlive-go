<template>
  <div v-if="isLoading" class="flex items-center justify-center h-screen">
    <v-progress-circular indeterminate color="primary" size="64" />
  </div>
  <div v-else>
    <slot v-if="!!data"></slot>
    <div v-else>
      <LoginForm />
    </div>
  </div>
</template>
<script setup lang="ts">
import LoginForm from '@/components/LoginForm.vue'
import { useAuthStore } from '@/stores/auth'
import { computed, onMounted, ref } from 'vue'

// Use store only once to avoid unnecessary reactivity operations
const authStore = useAuthStore()
const { setToken } = authStore
const isLoading = computed(() => authStore.isLoading)
const data = computed(() => authStore.data)

onMounted(async () => {
  try {
    const token = localStorage.getItem('token') || ''
    if (token) {
      setToken(token)
    }
  } catch (error) {
    console.error('Authentication error:', error)
  }
})
</script>
