import { ref } from 'vue'
import { client } from './client'

export const adminId = ref('')

export async function loadAppConfig(): Promise<void> {
  try {
    const res = await client.get('/config')
    const id = (res?.data?.admin_id ?? '').toString().trim().toLowerCase()
    adminId.value = id
  } catch (e) {
    console.warn('app config API unavailable', e)
    adminId.value = ''
  }
}