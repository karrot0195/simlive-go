import { API } from '@/constants'
import Panel from '@/views/Panel.vue'
import MappingDataPanel from '@/views/MappingDataPanel.vue'
import TmpDataPanel from '@/views/TmpDataPanel.vue'
import LogGroupViewer from '@/components/LogGroupViewer.vue'
import { MonitoringPanel, VMAction } from '@espx/ui'
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
      path: '/vm-monitoring',
      name: 'vm-monitoring',
      component: MonitoringPanel,
      props: { API: API }
    },
    {
      path: '/vm-action',
      name: 'vm-action',
      component: VMAction,
      props: { API: API },
    },
    {
      path: '/mapping-data',
      name: 'mapping-data',
      component: MappingDataPanel,
      props: { API: API },
    },
    {
      path: '/tmp-data',
      name: 'tmp-data',
      component: TmpDataPanel,
      props: { API: API },
    },
    {
      path: '/logs',
      name: 'logs',
      component: LogGroupViewer,
    }
  ]
})

export default router
