<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import '@espx/ui/index.css'
import MainProvider from '@/providers/main-provider.vue'

const router = useRouter()
const route = useRoute()

const isActive = (routeName) => {
  return route.name === routeName
}

const isSuperAdmin = localStorage.getItem('is_super_admin') === 'true'

const allMenuItems = [
  { name: 'channels', path: '/', label: 'Home', icon: 'mdi-home' }
]

const menuItems = computed(() => {
  return allMenuItems.filter((item) => !item.adminOnly || isSuperAdmin)
})
</script>
<template>
  <v-responsive class="border rounded" max-height="100%">
    <MainProvider>
      <v-app>
        <v-app-bar title="Simulated Live">
          <template #prepend>
            <img width="200px" src="https://static-cdn.espx.cloud/img/logo.png" alt="" />
          </template>
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
