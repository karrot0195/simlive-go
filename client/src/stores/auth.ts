import { defineStore } from 'pinia'
import { restClient } from '@/libs/client'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    isLoading: false,
    data: null as null | Record<string, any>,
    token: ''
  }),
  actions: {
    reset() {
      this.isLoading = false
      this.data = null
      this.token = ''
      localStorage.removeItem('token')
    },
    setToken(token: string) {
      if (!token) {
        this.reset()
        return
      }
      this.isLoading = true
      restClient
        .post('accounts/auth/verify-token', null, {
          headers: {
            Authorization: token
          }
        })
        .then((response) => {
          this.data = response.data
          this.token = token
          localStorage.setItem('token', token)
          localStorage.setItem('userInfo', JSON.stringify(response.data))
          localStorage.setItem('is_super_admin', String(response.data.sub === (import.meta.env.VITE_ADMIN_ID || '').toString().trim().toLowerCase()))
          console.log('Token verified successfully:', response.data)
        })
        .catch((error) => {
          console.error('Error verifying token:', error)
          this.data = null
          this.token = token
          localStorage.removeItem('token')
        })
        .finally(() => {
          this.isLoading = false
        })
    }
  }
})
