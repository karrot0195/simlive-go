<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import MainProvider from '@/providers/main-provider.vue'

const router = useRouter()
const route = useRoute()

const isActive = (routeName) => {
  return route.name === routeName
}

const adminId = (import.meta.env.VITE_ADMIN_ID || '').toString().trim().toLowerCase()

const getCurrentUserId = () => {
  try {
    const raw = localStorage.getItem('userInfo')
    if (!raw) return ''
    const parsed = JSON.parse(raw)
    const id = parsed?.sub ?? parsed?.id ?? parsed?.user_id ?? ''
    return id.toString().trim().toLowerCase()
  } catch {
    return ''
  }
}

const isAdmin = computed(() => !!adminId && getCurrentUserId() === adminId)

const allMenuItems = [
  { name: 'channels', path: '/', label: 'Home', icon: 'mdi-home' },
  { name: 'timeline', path: '/timeline', label: 'Running Timeline', icon: 'mdi-timeline-clock', adminOnly: true }
]

const menuItems = computed(() => {
  return allMenuItems.filter((item) => !item.adminOnly || isAdmin.value)
})
</script>
<template>
  <v-responsive class="border rounded" max-height="100%">
    <MainProvider>
      <v-app>
        <v-app-bar title="Simulated Live">
          <template #append>
            <v-btn
              v-for="item in menuItems"
              :key="item.name"
              :color="isActive(item.name) ? 'primary' : undefined"
              :variant="isActive(item.name) ? 'flat' : 'text'"
              @click="router.push(item.path)"
              class="mx-1"
            >
              <v-icon class="mr-2" size="small">{{ item.icon }}</v-icon>
              {{ item.label }}
            </v-btn>
          </template>
        </v-app-bar>
        <v-main>
          <RouterView />
        </v-main>
      </v-app>
    </MainProvider>
  </v-responsive>
</template>
