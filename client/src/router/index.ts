import Panel from '@/views/Panel.vue'
import RunningChannelsTimeline from '@/views/RunningChannelsTimeline.vue'
import { createRouter, createWebHistory } from 'vue-router'

import { adminId } from '@/libs/app-config'

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

router.beforeEach((to, from, next) => {
  if (to.name === 'timeline') {
    if (adminId.value && getCurrentUserId() === adminId.value) {
      next()
    } else {
      next('/')
    }
  } else {
    next()
  }
})

export default router
