<script setup lang="ts">
import { ref } from 'vue'
import { client } from '@/libs/client'
import { useAuthStore } from '@/stores/auth'

// API constants
const API_BASE_URL = 'https://msvc.espx.cloud'
const API_PATHS = {
  LOGIN: '/sso/auth/login',
  ACCOUNTS: '/sso/auth/accounts',
  ACCOUNT_TOKEN: '/sso/auth/account-token'
}

const { setToken } = useAuthStore()
const tempToken = ref('')

const login = ref({
  email: '',
  password: ''
})

const steper = ref(0)
const accounts = ref([])
const selectedAccount = ref<any>(null)

const handleLogin = async () => {
  try {
    const response = await client.post(`${API_BASE_URL}${API_PATHS.LOGIN}`, {
      username: login.value.email,
      password: login.value.password
    })

    tempToken.value = response.data.access_token

    accounts.value = (
      await client.get(`${API_BASE_URL}${API_PATHS.ACCOUNTS}`, {
        headers: {
          Authorization: `Bearer ${tempToken.value}`
        }
      })
    ).data

    steper.value = 1

    console.log('Login successful:', accounts.value)
  } catch (error) {
    console.error('Login error:', error)
  }
}

const handleUpdateToken = async () => {
  const response = await client.post(
    `${API_BASE_URL}${API_PATHS.ACCOUNT_TOKEN}`,
    {
      accountId: selectedAccount.value
    },
    {
      headers: {
        Authorization: `Bearer ${tempToken.value}`
      }
    }
  )

  const token = response.data.access_token
  setToken(token)
}
</script>

<template>
  <div class="flex flex-col items-center justify-center w-full h-screen p-4">
    <div class="w-full max-w-lg">
      <v-card>
        <v-card-title class="text-h5">Login</v-card-title>
        <v-card-text>
          <div v-if="steper === 0">
            <v-text-field v-model="login.email" label="email" type="email" outlined class="w-full"
              :rules="[(v) => !!v || 'Email is required']"></v-text-field>
            <v-text-field v-model="login.password" label="password" type="password" outlined class="w-full"
              :rules="[(v) => !!v || 'Password is required']"></v-text-field>
            <v-btn class="mt-3" :disabled="!login.email || !login.password" @click="handleLogin" color="primary"
              block>Login</v-btn>
          </div>
          <div v-if="steper == 1">
            <label for="">Select account</label>
            <v-select v-model="selectedAccount" :items="accounts" item-value="id" item-title="name"
              label="Select account" outlined class="w-full"></v-select>
            <v-btn class="mt-3" :class="selectedAccount ? 'bg-primary' : 'bg-gray-300'" :disabled="!selectedAccount"
              @click="handleUpdateToken" color="primary" block>Auth</v-btn>
          </div>
        </v-card-text>
      </v-card>
    </div>
  </div>
</template>
