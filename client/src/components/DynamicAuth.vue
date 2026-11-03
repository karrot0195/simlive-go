<template>
  <div class="login-shell">
    <div class="login-bg" aria-hidden="true"></div>
    <v-card class="login-card" elevation="0" rounded="xl">
      <div class="login-accent"></div>
      <v-card-text class="pa-8">
        <div class="text-center mb-6">
          <v-avatar size="64" color="primary" variant="tonal" class="mb-3">
            <v-icon size="36" color="primary">mdi-lock-outline</v-icon>
          </v-avatar>
          <h1 class="text-h5 font-weight-bold mb-1">{{ currentStep?.title || 'Sign in' }}</h1>
          <p class="text-body-2 text-medium-emphasis mb-0">
            Enter your credentials to continue
          </p>
        </div>

        <v-alert
          v-if="error"
          type="error"
          variant="tonal"
          density="comfortable"
          class="mb-4"
          closable
          @click:close="error = ''"
        >
          {{ error }}
        </v-alert>

        <template v-if="currentStep">
          <template v-for="f in currentStep.fields || []" :key="f.name">
            <v-text-field
              v-if="f.type !== 'select'"
              v-model="form[f.name]"
              :label="f.label"
              :type="fieldType(f)"
              :prepend-inner-icon="fieldIcon(f)"
              :append-inner-icon="f.type === 'password' ? (showPassword ? 'mdi-eye-off' : 'mdi-eye') : undefined"
              :required="f.required"
              variant="outlined"
              density="comfortable"
              color="primary"
              class="mb-4"
              hide-details="auto"
              autocomplete="off"
              @click:append-inner="f.type === 'password' && (showPassword = !showPassword)"
              @keyup.enter="submit"
            />
            <v-select
              v-else
              v-model="form[f.name]"
              :label="f.label"
              :items="resolvedOptions[f.name] || []"
              :item-title="(item: any) => getOptionLabel(item, f)"
              :item-value="(item: any) => getOptionValue(item, f)"
              :required="f.required"
              variant="outlined"
              density="comfortable"
              color="primary"
              class="mb-4"
              hide-details="auto"
            />
          </template>

          <div class="d-flex flex-row gap-3 mt-2">
            <v-btn
              v-if="stepIndex > 0"
              variant="outlined"
              color="primary"
              size="large"
              class="flex-grow-1 text-none"
              :disabled="loading"
              @click="goBack"
            >
              Back
            </v-btn>
            <v-btn
              color="primary"
              size="large"
              class="flex-grow-1 text-none font-weight-semibold"
              :loading="loading"
              :disabled="loading"
              elevation="2"
              @click="submit"
            >
              {{ stepIndex > 0 && currentStep?.fields?.length ? 'Continue' : 'Sign in' }}
            </v-btn>
          </div>
        </template>

        <div v-else class="text-center text-medium-emphasis py-6">
          No authentication configuration available.
        </div>
      </v-card-text>
    </v-card>

    <p class="login-footer text-caption text-medium-emphasis text-center mt-6">
      Secured connection &middot; Simulated Live
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { fetchAuthConfig, resolvePlaceholders, runPreCalls, getByPath, resolveOptions, getOptionLabel, getOptionValue, type AuthConfig, type AuthStep } from '@/libs/auth-config'

const auth = useAuthStore()
const config = ref<AuthConfig | null>(null)
const stepIndex = ref(0)
const form = ref<Record<string, any>>({})
const forms = ref<Record<number, Record<string, any>>>({})
const loading = ref(false)
const error = ref('')
const resolvedOptions = ref<Record<string, any[]>>({})
const showPassword = ref(false)

const currentStep = computed<AuthStep | null>(() => {
  if (!config.value || !config.value.steps) return null
  return config.value.steps[stepIndex.value] || null
})

function fieldType(f: any): string {
  if (f.type === 'password') return showPassword.value ? 'text' : 'password'
  return 'text'
}

function fieldIcon(f: any): string {
  if (f.type === 'password') return 'mdi-lock-outline'
  const name = String(f.name || '').toLowerCase()
  if (name.includes('user') || name.includes('email') || name.includes('account')) return 'mdi-account-outline'
  return 'mdi-form-textbox'
}

onMounted(async () => {
  config.value = await fetchAuthConfig()
  await enterStep(0)
})

function buildCtx(extra?: Record<string, any>): Record<string, any> {
  const ctx: Record<string, any> = { ...auth.tokens, token: auth.token }
  for (const fm of Object.values(forms.value)) {
    Object.assign(ctx, fm)
  }
  return { ...ctx, ...(extra || {}) }
}

function resolveStepOptions(s: AuthStep, ctx: Record<string, any>) {
  resolvedOptions.value = {}
  for (const field of s.fields || []) {
    if (field.type === 'select' && (field as any).options) {
      resolvedOptions.value[field.name] = resolveOptions((field as any).options, ctx)
    }
  }
}

async function enterStep(index: number) {
  const s = config.value?.steps[index]
  if (!s) return
  stepIndex.value = index
  error.value = ''
  showPassword.value = false
  const snap = forms.value[index]
  const f: Record<string, any> = {}
  for (const field of s.fields || []) {
    f[field.name] = snap?.[field.name] ?? field.default ?? ''
  }
  form.value = f
  resolvedOptions.value = {}
  loading.value = true
  try {
    const ctx = buildCtx()
    await runPreCalls(s.preCalls, ctx)
    resolveStepOptions(s, ctx)
  } catch (e: any) {
    error.value = e?.message || 'Failed to load step'
  } finally {
    loading.value = false
  }
}

function goBack() {
  if (stepIndex.value <= 0) return
  forms.value[stepIndex.value] = { ...form.value }
  enterStep(stepIndex.value - 1)
}

async function submit() {
  const s = currentStep.value
  if (!s) return
  error.value = ''
  for (const f of s.fields || []) {
    const val = form.value[f.name]
    if (f.required && !val) {
      error.value = `${f.label} is required`
      return
    }
    const rules = (s.validation && s.validation[f.name]) || {}
    if (rules.pattern && val && !new RegExp(rules.pattern).test(String(val))) {
      error.value = `${f.label} is invalid`
      return
    }
    if (rules.minLength && String(val || '').length < rules.minLength) {
      error.value = `${f.label} is too short`
      return
    }
    if (rules.maxLength && String(val || '').length > rules.maxLength) {
      error.value = `${f.label} is too long`
      return
    }
  }
  loading.value = true
  try {
    const ctx = buildCtx(form.value)
    await runPreCalls(s.preCalls, ctx)
    resolveStepOptions(s, ctx)
    const url = resolvePlaceholders(s.api, ctx)
    const headers = resolvePlaceholders(s.headers || {}, ctx)
    const body = resolvePlaceholders(s.body, ctx)
    const res = await fetch(url, {
      method: s.method,
      headers: { 'Content-Type': 'application/json', ...headers },
      body: s.method !== 'GET' ? JSON.stringify(body ?? {}) : undefined,
    })
    if (!res.ok) throw new Error(`Request failed: ${res.status}`)
    const data = await res.json()
    const token = getByPath(data, s.response_field)
    if (token) auth.accumulateToken(s.response_field, String(token))
    forms.value[stepIndex.value] = { ...form.value }
    if (config.value && stepIndex.value < config.value.steps.length - 1) {
      await enterStep(stepIndex.value + 1)
    } else {
      const ok = await auth.verifyToken()
      if (!ok) error.value = 'Token verification failed'
    }
  } catch (e: any) {
    error.value = e?.message || 'Step failed'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-shell {
  position: relative;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 24px;
  overflow: hidden;
}

.login-bg {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(1200px 600px at 10% 10%, rgba(37, 99, 235, 0.12), transparent 60%),
    radial-gradient(900px 500px at 90% 90%, rgba(99, 102, 241, 0.10), transparent 60%),
    linear-gradient(135deg, #f1f5f9 0%, #e2e8f0 100%);
  z-index: 0;
}

.login-card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 420px;
  background: #ffffff;
  border: 1px solid rgba(15, 23, 42, 0.06);
  box-shadow: 0 20px 45px -12px rgba(15, 23, 42, 0.18), 0 4px 12px rgba(15, 23, 42, 0.06);
}

.login-accent {
  height: 4px;
  background: linear-gradient(90deg, #2563eb 0%, #6366f1 100%);
}

.login-footer {
  position: relative;
  z-index: 1;
}

/* Comfortable, accessible typography */
.v-card .v-field__input,
.v-card .v-select__selection-text,
.v-card .v-field-label {
  font-size: 15px;
}

.v-btn {
  letter-spacing: 0.3px;
  text-transform: none;
}
</style>