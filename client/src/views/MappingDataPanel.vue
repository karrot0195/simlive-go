<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { client } from '@/libs/client'
// @ts-ignore
import VueJsonViewer from 'vue-json-viewer'
import 'vue-json-viewer/style.css'

const route = useRoute()
const router = useRouter()

const redisKey = ref('')
const redisData = ref<any>(null)
const loading = ref(false)
const loadingKeys = ref(false)
const error = ref('')
const autoRefresh = ref(false)
const refreshInterval = ref(5)
let intervalId: NodeJS.Timeout | null = null

const searchPattern = ref('*')
const availableKeys = ref<string[]>([])
const showKeyList = ref(false)

const sortedKeys = computed(() => {
  const keys = [...availableKeys.value]
  return keys.sort((a, b) => {
    // Check if keys contain 'metadata'
    const aHasMetadata = a.includes(':metadata')
    const bHasMetadata = b.includes(':metadata')

    // metadata keys always come first
    if (aHasMetadata && !bHasMetadata) return -1
    if (!aHasMetadata && bHasMetadata) return 1

    // Otherwise sort alphabetically
    return a.localeCompare(b)
  })
})

const formatValue = (value: any, type: string) => {
  if (value === null || value === undefined) {
    return 'null'
  }

  try {
    if (typeof value === 'object') {
      return JSON.stringify(value, null, 2)
    }
    return String(value)
  } catch {
    return String(value)
  }
}

const formattedValue = computed(() => {
  if (!redisData.value) return ''
  return formatValue(redisData.value.value, redisData.value.type)
})

const formatDate = (t: string, beforeDate?: string) => {
  // if beforeDate and t is same date, show only time
  if (beforeDate) {
    const before = new Date(beforeDate)
    if (before.toDateString() === new Date(t).toDateString()) {
      const date = new Date(t)
      return date.toLocaleTimeString()
    }
  }
  const date = new Date(t)
  return date.toLocaleString()
}

const hasDateFields = (value: any): value is { start?: string; end?: string } => {
  return value !== null && typeof value === 'object' && ('start' in value || 'end' in value)
}

const isObjectType = computed(() => {
  if (!redisData.value || !redisData.value.value) {
    console.log('isObjectType: false - no data', redisData.value)
    return false
  }
  const value = redisData.value.value
  const result = typeof value === 'object' && value !== null && Object.keys(value).length > 0
  console.log('isObjectType check:', {
    value,
    typeofValue: typeof value,
    isNull: value === null,
    keysLength: Object.keys(value || {}).length,
    result
  })
  return result
})

const sortedHashFields = computed(() => {
  if (!redisData.value || !redisData.value.value || redisData.value.type !== 'hash') {
    return []
  }

  const entries = Object.entries(redisData.value.value)
  return entries.sort(([keyA], [keyB]) => {
    // Check if keys contain 'metadata'
    const aHasMetadata = keyA.includes(':metadata') || keyA.includes('metadata')
    const bHasMetadata = keyB.includes(':metadata') || keyB.includes('metadata')

    // metadata keys always come first
    if (aHasMetadata && !bHasMetadata) return -1
    if (!aHasMetadata && bHasMetadata) return 1

    // Otherwise sort alphabetically/numerically
    return keyA.localeCompare(keyB, undefined, { numeric: true })
  })
})

const fetchRedisData = async () => {
  if (!redisKey.value.trim()) {
    error.value = 'Please enter a Redis key'
    return
  }

  loading.value = true
  error.value = ''

  // Update URL query params
  router.push({
    query: {
      ...route.query,
      key: redisKey.value
    }
  })

  try {
    const response = await client.get(`/redis/get-key`, {
      params: { key: redisKey.value }
    })
    console.log('Redis response:', response.data)
    redisData.value = response.data

    if (!response.data.exists) {
      error.value = `Key "${redisKey.value}" does not exist in Redis`
      redisData.value = null
    }
  } catch (err: any) {
    error.value = err.response?.data?.message || 'Failed to fetch Redis data'
    redisData.value = null
  } finally {
    loading.value = false
  }
}

const searchKeys = async () => {
  if (!searchPattern.value.trim()) {
    searchPattern.value = '*'
  }

  loadingKeys.value = true
  error.value = ''
  showKeyList.value = true

  try {
    const response = await client.get(`/redis/keys`, {
      params: { pattern: searchPattern.value }
    })
    availableKeys.value = response.data.keys

    if (availableKeys.value.length === 0) {
      error.value = `No keys found matching pattern "${searchPattern.value}"`
    }
  } catch (err: any) {
    error.value = err.response?.data?.message || 'Failed to search keys'
    availableKeys.value = []
  } finally {
    loadingKeys.value = false
  }
}

const selectKey = (key: string) => {
  redisKey.value = key
  showKeyList.value = false
  fetchRedisData()
}

const clearData = () => {
  redisData.value = null
  error.value = ''
}

const copyToClipboard = async () => {
  try {
    await navigator.clipboard.writeText(formattedValue.value)
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}

const getTypeColor = (type: string) => {
  const colors: Record<string, string> = {
    string: 'primary',
    list: 'success',
    set: 'warning',
    zset: 'info',
    hash: 'purple',
    none: 'error'
  }
  return colors[type] || 'secondary'
}

const startAutoRefresh = () => {
  if (intervalId) {
    clearInterval(intervalId)
  }

  if (autoRefresh.value && redisKey.value) {
    intervalId = setInterval(() => {
      fetchRedisData()
    }, refreshInterval.value * 1000)
  }
}

watch([autoRefresh, refreshInterval], () => {
  startAutoRefresh()
})

watch(redisKey, () => {
  if (autoRefresh.value) {
    startAutoRefresh()
  }
})

onMounted(() => {
  // Load all keys on mount
  searchKeys()

  // Check if there's a key in query params
  const queryKey = route.query.key as string
  if (queryKey) {
    redisKey.value = queryKey
    fetchRedisData()
  }
})

onBeforeUnmount(() => {
  if (intervalId) {
    clearInterval(intervalId)
  }
})
</script>

<template>
  <v-container fluid class="pa-6">
    <v-card>
      <v-card-title class="bg-primary">
        <v-icon class="mr-2">mdi-database-eye</v-icon>
        Redis Data Viewer
      </v-card-title>

      <v-card-text class="pa-6">
        <!-- Search Keys Section -->
        <v-row class="mb-4">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-title class="text-subtitle-1">
                <v-icon class="mr-2" size="small">mdi-magnify</v-icon>
                Search Keys
              </v-card-title>
              <v-card-text>
                <v-row>
                  <v-col cols="12" md="9">
                    <v-text-field
                      v-model="searchPattern"
                      label="Search Pattern"
                      placeholder="e.g., user:*, session:*, or *"
                      variant="outlined"
                      density="comfortable"
                      hint="Use * as wildcard. Examples: user:*, *:session, *"
                      persistent-hint
                      @keyup.enter="searchKeys"
                    >
                      <template #prepend-inner>
                        <v-icon>mdi-key-variant</v-icon>
                      </template>
                    </v-text-field>
                  </v-col>
                  <v-col cols="12" md="3" class="d-flex align-center">
                    <v-btn
                      color="primary"
                      @click="searchKeys"
                      :loading="loadingKeys"
                      block
                      size="large"
                    >
                      <v-icon class="mr-2">mdi-refresh</v-icon>
                      Reload Keys
                    </v-btn>
                  </v-col>
                </v-row>

                <!-- Keys List -->
                <v-expand-transition>
                  <div v-if="showKeyList && availableKeys.length > 0">
                    <v-divider class="my-4"></v-divider>
                    <div class="text-subtitle-2 mb-2">Found {{ availableKeys.length }} key(s):</div>
                    <v-list density="compact" max-height="200" class="overflow-y-auto">
                      <v-list-item
                        v-for="key in sortedKeys"
                        :key="key"
                        @click="selectKey(key)"
                        class="cursor-pointer"
                      >
                        <template #prepend>
                          <v-icon size="small">mdi-key</v-icon>
                        </template>
                        <v-list-item-title>{{ key }}</v-list-item-title>
                      </v-list-item>
                    </v-list>
                  </div>
                </v-expand-transition>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <!-- Get Key Section -->
        <v-row class="mb-4">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-title class="text-subtitle-1">
                <v-icon class="mr-2" size="small">mdi-database-search</v-icon>
                Get Key Value
              </v-card-title>
              <v-card-text>
                <v-row>
                  <v-col cols="12" md="9">
                    <v-autocomplete
                      v-model="redisKey"
                      :items="sortedKeys"
                      label="Select Redis Key"
                      placeholder="Choose a key from the list"
                      variant="outlined"
                      density="comfortable"
                      clearable
                      :loading="loadingKeys"
                      @update:model-value="fetchRedisData"
                    >
                      <template #prepend-inner>
                        <v-icon>mdi-key-variant</v-icon>
                      </template>
                      <template #item="{ props, item }">
                        <v-list-item v-bind="props">
                          <template #prepend>
                            <v-icon size="small">mdi-key</v-icon>
                          </template>
                        </v-list-item>
                      </template>
                    </v-autocomplete>
                  </v-col>
                  <v-col cols="12" md="3" class="d-flex align-center">
                    <v-btn
                      color="success"
                      @click="fetchRedisData"
                      :loading="loading"
                      :disabled="!redisKey.trim()"
                      block
                      size="large"
                    >
                      <v-icon class="mr-2">mdi-download</v-icon>
                      Fetch
                    </v-btn>
                  </v-col>
                </v-row>

                <!-- Auto Refresh Controls -->
                <v-row class="mt-2">
                  <v-col cols="12" md="6">
                    <v-switch
                      v-model="autoRefresh"
                      label="Auto Refresh"
                      color="primary"
                      density="comfortable"
                      hide-details
                    >
                      <template #prepend>
                        <v-icon>mdi-refresh-auto</v-icon>
                      </template>
                    </v-switch>
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-select
                      v-model="refreshInterval"
                      :items="[1, 2, 5, 10, 30, 60]"
                      label="Refresh Interval (seconds)"
                      variant="outlined"
                      density="comfortable"
                      :disabled="!autoRefresh"
                      hide-details
                    ></v-select>
                  </v-col>
                </v-row>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <!-- Error Alert -->
        <v-alert v-if="error" type="error" variant="tonal" class="mb-4">
          <v-icon class="mr-2">mdi-alert-circle</v-icon>
          {{ error }}
        </v-alert>

        <!-- Data Display Section -->
        <v-row v-if="redisData && redisData.exists">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-title class="d-flex justify-space-between align-center">
                <div>
                  <v-chip :color="getTypeColor(redisData.type)" size="small" class="mr-2">
                    {{ redisData.type.toUpperCase() }}
                  </v-chip>
                  <span class="text-subtitle-1">{{ redisData.key }}</span>
                </div>
                <div>
                  <v-chip v-if="redisData.ttl" color="warning" size="small" class="mr-2">
                    <v-icon size="small" class="mr-1">mdi-clock-outline</v-icon>
                    TTL: {{ redisData.ttl }}s
                  </v-chip>
                  <v-btn
                    icon="mdi-content-copy"
                    size="small"
                    variant="text"
                    @click="copyToClipboard"
                    title="Copy to clipboard"
                  ></v-btn>
                  <v-btn
                    icon="mdi-close"
                    size="small"
                    variant="text"
                    @click="clearData"
                    title="Clear"
                  ></v-btn>
                </div>
              </v-card-title>

              <v-divider></v-divider>

              <v-card-text>
                <div v-if="isObjectType && redisData.type === 'hash'" class="hash-viewer">
                  <v-expansion-panels variant="accordion">
                    <v-expansion-panel
                      v-for="[key, value] in sortedHashFields"
                      :key="key"
                      :value="key"
                    >
                      <v-expansion-panel-title>
                        <div class="d-flex align-center justify-space-between w-100 pr-4">
                          <div class="d-flex align-center">
                            <v-icon size="small" class="mr-2">mdi-key</v-icon>
                            <strong class="mr-3">{{ key }}</strong>
                            <v-chip size="x-small" color="info" variant="flat">
                              {{ typeof value === 'object' ? 'Object' : typeof value }}
                            </v-chip>
                          </div>
                          <div v-if="hasDateFields(value)" class="date-range-display">
                            <div v-if="value.start" class="date-item">
                              <v-icon size="x-small" class="mr-1">mdi-calendar-start</v-icon>
                              {{ formatDate(value.start) }}
                            </div>
                            <v-icon v-if="value.start && value.end" size="x-small" class="mx-1">
                              mdi-arrow-right
                            </v-icon>
                            <div v-if="value.end" class="date-item">
                              <v-icon size="x-small" class="mr-1">mdi-calendar-end</v-icon>
                              {{ formatDate(value.end, value.start) }}
                            </div>
                          </div>
                        </div>
                      </v-expansion-panel-title>
                      <v-expansion-panel-text>
                        <div class="json-viewer-container">
                          <VueJsonViewer :value="value" :expand-depth="12" copyable boxed />
                        </div>
                      </v-expansion-panel-text>
                    </v-expansion-panel>
                  </v-expansion-panels>
                </div>
                <div v-else-if="isObjectType" class="json-viewer-container">
                  <VueJsonViewer :value="redisData.value" :expand-depth="1" copyable boxed />
                </div>
                <pre v-else class="redis-value-display">{{ formattedValue }}</pre>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <!-- Empty State -->
        <v-row v-if="!redisData && !error && !loading">
          <v-col cols="12">
            <v-card variant="outlined" class="text-center pa-8">
              <v-icon size="64" color="grey-lighten-1">mdi-database-search</v-icon>
              <div class="text-h6 mt-4 text-grey">No Data</div>
              <div class="text-body-2 text-grey">
                Enter a Redis key or search for keys to view data
              </div>
            </v-card>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>
  </v-container>
</template>

<style scoped>
.hash-viewer {
  max-height: 600px;
  overflow-y: auto;
}

.json-viewer-container {
  background-color: #f5f5f5;
  border: 1px solid #e0e0e0;
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
  max-height: 500px;
  overflow-y: auto;
}

.redis-value-display {
  background-color: #f5f5f5;
  border: 1px solid #e0e0e0;
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
  font-family: 'Courier New', monospace;
  font-size: 14px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-wrap: break-word;
  max-height: 600px;
  overflow-y: auto;
}

.cursor-pointer {
  cursor: pointer;
}

.cursor-pointer:hover {
  background-color: rgba(0, 0, 0, 0.04);
}

.date-range-display {
  display: flex;
  align-items: center;
  font-size: 0.75rem;
  color: #757575;
  white-space: nowrap;
}

.date-item {
  display: flex;
  align-items: center;
  padding: 2px 8px;
  background-color: rgba(0, 0, 0, 0.05);
  border-radius: 4px;
}
</style>
