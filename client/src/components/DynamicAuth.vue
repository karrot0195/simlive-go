<template>
  <div class="d-flex align-center justify-center" style="min-height: 100vh; background: linear-gradient(135deg, #f5f7fa 0%, #c3cfe2 100%)">
    <v-card
      class="pa-6"
      :min-width="stepStyle?.card?.minWidth || 400"
      :max-width="500"
      elevation="8"
      rounded="lg"
      :style="cardStyle"
    >
      <v-card-title class="text-h5 font-weight-bold text-center pa-4" :style="titleStyle">
        {{ currentStep?.title || 'Login' }}
      </v-card-title>
      <v-divider class="mb-4"></v-divider>
      <v-card-text class="pa-4">
        <v-alert
          v-if="error"
          type="error"
          variant="tonal"
          density="compact"
          class="mb-4"
        >
          {{ error }}
        </v-alert>
        <template v-if="currentStep">
          <template v-for="f in currentStep.fields || []" :key="f.name">
            <v-select
              v-if="f.type === 'select'"
              v-model="form[f.name]"
              :label="f.label"
              :items="resolvedOptions[f.name] || []"
              :item-title="(item: any) => getOptionLabel(item, f)"
              :item-value="(item: any) => getOptionValue(item, f)"
              :required="f.required"
              variant="outlined"
              density="comfortable"
              class="mb-3"
              hide-details="auto"
            />
            <v-text-field
              v-else
              v-model="form[f.name]"
              :label="f.label"
              :type="f.type === 'password' ? 'password' : 'text'"
              :required="f.required"
              variant="outlined"
              density="comfortable"
              class="mb-3"
              hide-details="auto"
            />
          </template>
          <div class="d-flex flex-row gap-3 mt-4">
            <v-btn
              v-if="stepIndex > 0"
              variant="outlined"
              size="large"
              class="flex-grow-1"
              :disabled="loading"
              @click="goBack"
            >
              Back
            </v-btn>
            <v-btn
              :color="stepStyle?.button?.color || 'primary'"
              size="large"
              class="flex-grow-1"
              :loading="loading"
              @click="submit"
            >
              Continue
            </v-btn>
          </div>
        </template>
        <div v-else class="text-center text-medium-emphasis">No auth configuration available.</div>
     </v-card-text>
   </v-card>
 </div>

<style scoped>
/* Make the login form more comfortable to read */
.v-card {
  font-size: 16px;
}
.v-card-title {
  font-size: 1.5rem !important;
}
.v-field__input,
.v-select__selection-text,
.v-field-label {
  font-size: 16px !important;
}
.v-btn {
  font-size: 16px !important;
  letter-spacing: 0.5px;
}
</style>
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

const currentStep = computed<AuthStep | null>(() => {
  if (!config.value || !config.value.steps) return null
  return config.value.steps[stepIndex.value] || null
})

const stepStyle = computed(() => (currentStep.value as any)?.style || (config.value as any)?.style)
const cardStyle = computed(() => stepStyle.value?.card?.style || {})
const titleStyle = computed(() => stepStyle.value?.title?.style || {})

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
