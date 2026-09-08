import Panel from '@/views/Panel.vue'
import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'channels',
      component: Panel
    }
  ]
})

export default router
