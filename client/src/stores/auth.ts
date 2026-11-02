import { defineStore } from 'pinia'
import { restClient } from '@/libs/client'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    isLoading: false,
    data: null as null | Record<string, any>,
    token: localStorage.getItem('token') || '',
    tokens: JSON.parse(localStorage.getItem('tokens') || '{}') as Record<string, string>,
  }),
  actions: {
    reset() {
      this.isLoading = false
      this.data = null
      this.token = ''
      this.tokens = {}
      localStorage.removeItem('token')
      localStorage.removeItem('tokens')
      localStorage.removeItem('userInfo')
      localStorage.removeItem('is_super_admin')
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
            Authorization: token,
          },
        })
        .then((response) => {
          this.data = response.data
          this.token = token
          localStorage.setItem('token', token)
          localStorage.setItem('userInfo', JSON.stringify(response.data))
          localStorage.setItem(
            'is_super_admin',
            String(
              response.data.sub ===
                (import.meta.env.VITE_ADMIN_ID || '').toString().trim().toLowerCase()
            )
          )
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
    },
    async verifyToken(): Promise<boolean> {
      const token = localStorage.getItem('token') || this.token
      if (!token) {
        this.reset()
        return false
      }
      this.isLoading = true
      let clearOnError = true
      try {
        const { fetchAuthConfig, resolvePlaceholders, getByPath } = await import('@/libs/auth-config')
        const cfg: any = await fetchAuthConfig()
        const verify = cfg?.verify
        if (!verify?.url) {
          await this.setToken(token)
          return true
        }
        if (verify.clear_on_error === false) {
          clearOnError = false
        }
        const ctx: Record<string, any> = { token }
        const headers: any = resolvePlaceholders({ ...(verify.headers || {}) }, ctx)
        if (!headers.Authorization) {
          headers.Authorization = `Bearer ${token}`
        }
        const method = (verify.method || 'POST').toUpperCase()
        const res = await fetch(verify.url, {
          method,
          headers: { 'Content-Type': 'application/json', ...headers },
          body:
            method !== 'GET'
              ? JSON.stringify(resolvePlaceholders(verify.body ?? {}, ctx))
              : undefined,
        })
        if (!res.ok) throw new Error(`verify failed: ${res.status}`)
        const data = await res.json().catch(() => ({}))
        if (verify.response_ok && !getByPath(data, verify.response_ok)) {
          throw new Error('verify response not ok')
        }
        this.data = data
        this.token = token
        localStorage.setItem('token', token)
        localStorage.setItem('userInfo', JSON.stringify(data))
        localStorage.setItem(
          'is_super_admin',
          String(
            data.sub ===
              (import.meta.env.VITE_ADMIN_ID || '').toString().trim().toLowerCase()
          )
        )
        return true
      } catch (e) {
        console.error('verify failed', e)
        if (clearOnError) {
          this.reset()
          if (typeof window !== 'undefined') {
            window.location.href = '/'
          }
        } else {
          this.token = token
          if (!this.data) this.data = {}
        }
        return !clearOnError
      } finally {
        this.isLoading = false
      }
    },
    accumulateToken(key: string, value: string) {
      this.tokens[key] = value
      this.token = value
      localStorage.setItem('tokens', JSON.stringify(this.tokens))
      localStorage.setItem('token', value)
    },
  },
})
