import Panel from '@/views/Panel.vue'
import RunningChannelsTimeline from '@/views/RunningChannelsTimeline.vue'
import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'channels',
      component: Panel
    },
    {
      path: '/timeline',
      name: 'timeline',
      component: RunningChannelsTimeline
    }
  ]
})

export default router
