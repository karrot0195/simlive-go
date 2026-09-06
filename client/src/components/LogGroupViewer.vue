<template>
  <div class="log-group-viewer h-screen flex flex-col bg-gray-50">
    <!-- Header -->
    <div class="bg-white shadow-sm border-b border-gray-200 p-4">
      <div class="flex justify-between items-center">
        <h1 class="text-2xl font-bold text-gray-800">Log Context Viewer</h1>
        <div class="flex gap-3 items-center">
          <label class="flex items-center gap-2 text-sm text-gray-600">
            <input
              v-model="autoRefresh"
              type="checkbox"
              class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            Auto-refresh
          </label>
          <select
            v-model="refreshInterval"
            class="text-sm border border-gray-300 rounded-md px-3 py-1.5"
            :disabled="!autoRefresh"
          >
            <option :value="2">2s</option>
            <option :value="5">5s</option>
            <option :value="10">10s</option>
          </select>
          <button
            @click="fetchLogs"
            class="px-4 py-1.5 bg-blue-600 text-white rounded-md hover:bg-blue-700 text-sm"
          >
            Refresh Now
          </button>
          <button
            @click="showSettings = !showSettings"
            class="px-4 py-1.5 bg-gray-600 text-white rounded-md hover:bg-gray-700 text-sm"
          >
            Settings
          </button>
        </div>
      </div>

      <!-- Filters -->
      <div class="mt-4 flex gap-3 items-center">
        <input
          v-model="searchFilter"
          type="text"
          placeholder="Search logs..."
          class="flex-1 px-3 py-2 border border-gray-300 rounded-md text-sm"
        />
        <div class="flex gap-2">
          <button
            v-for="level in ['ERROR', 'WARN', 'INFO', 'DEBUG']"
            :key="level"
            @click="toggleLevel(level)"
            :class="[
              'px-3 py-1.5 rounded-md text-sm font-medium transition-colors',
              levelFilter.includes(level)
                ? levelColors[level] + ' text-white'
                : 'bg-gray-200 text-gray-600'
            ]"
          >
            {{ level }}
          </button>
        </div>
      </div>
    </div>

    <!-- Settings Panel -->
    <div v-if="showSettings" class="bg-white border-b border-gray-200 p-4">
      <h2 class="text-lg font-semibold mb-3">Section Settings</h2>
      <div class="grid grid-cols-2 gap-3 max-h-60 overflow-y-auto">
        <div
          v-for="context in allContextIds"
          :key="context"
          class="flex items-center justify-between p-2 bg-gray-50 rounded border"
        >
          <span class="text-sm font-mono truncate flex-1">{{ context }}</span>
          <div class="flex gap-2">
            <button
              @click="moveUp(context)"
              class="px-2 py-1 bg-gray-300 hover:bg-gray-400 rounded text-xs"
              :disabled="getContextOrder(context) === 0"
            >
              ↑
            </button>
            <button
              @click="moveDown(context)"
              class="px-2 py-1 bg-gray-300 hover:bg-gray-400 rounded text-xs"
              :disabled="getContextOrder(context) === allContextIds.length - 1"
            >
              ↓
            </button>
            <button
              @click="toggleVisibility(context)"
              :class="[
                'px-2 py-1 rounded text-xs',
                isVisible(context)
                  ? 'bg-green-600 hover:bg-green-700 text-white'
                  : 'bg-red-600 hover:bg-red-700 text-white'
              ]"
            >
              {{ isVisible(context) ? 'Hide' : 'Show' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Log Sections Grid -->
    <div class="flex-1 overflow-y-auto p-4">
      <div class="grid grid-cols-2 gap-4">
        <div
          v-for="context in visibleContextsSorted"
          :key="context"
          class="bg-white rounded-lg shadow border border-gray-200 flex flex-col overflow-hidden"
          style="height: 500px"
        >
          <!-- Section Header -->
          <div
            class="bg-gradient-to-r from-blue-600 to-blue-700 text-white px-4 py-3 flex justify-between items-center"
          >
            <div class="flex-1 min-w-0">
              <h3 class="font-semibold text-sm truncate" :title="context">{{ context }}</h3>
              <p class="text-xs text-blue-100">{{ getContextLogCount(context) }} logs</p>
            </div>
            <button
              @click="toggleVisibility(context)"
              class="ml-2 px-2 py-1 bg-white/20 hover:bg-white/30 rounded text-xs"
            >
              Hide
            </button>
          </div>

          <!-- Log Content -->
          <div class="flex-1 overflow-y-auto p-3 bg-gray-50 font-mono text-xs">
            <div
              v-for="(log, index) in getContextLogs(context)"
              :key="index"
              :class="['p-2 mb-1 rounded border-l-4', getLevelClass(log)]"
            >
              <div class="flex gap-2">
                <span class="text-gray-500">{{ formatTime(log.time) }}</span>
                <span :class="getLevelTextClass(log.level)">{{ getLevelLabel(log.level) }}</span>
              </div>
              <div
                class="mt-1 text-gray-800 whitespace-pre-wrap break-words"
                v-html="highlightText(log.msg)"
              ></div>
            </div>
            <div v-if="getContextLogs(context).length === 0" class="text-gray-400 text-center py-8">
              No logs matching filters
            </div>
          </div>
        </div>
      </div>

      <div v-if="visibleContextsSorted.length === 0" class="text-center py-12 text-gray-500">
        <p class="text-lg">No visible log contexts</p>
        <p class="text-sm mt-2">Enable sections in settings or adjust filters</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useLogSectionSettings } from '@/composables/useLogSectionSettings'

interface LogEntry {
  level: number
  time: number
  pid?: number
  hostname?: string
  context?: string
  msg: string
  [key: string]: any
}

const API = import.meta.env.VITE_API_URL || 'http://localhost:3000'

const logs = ref<LogEntry[]>([])
const searchFilter = ref('')
const levelFilter = ref<string[]>(['ERROR', 'WARN', 'INFO', 'DEBUG'])
const autoRefresh = ref(true)
const refreshInterval = ref(5)
const showSettings = ref(false)
let refreshTimer: NodeJS.Timeout | null = null

const {
  sectionSettings,
  getSectionSettings,
  toggleSectionVisibility,
  updateSectionOrder,
  reorderSections
} = useLogSectionSettings()

const levelColors: Record<string, string> = {
  ERROR: 'bg-red-600',
  WARN: 'bg-yellow-600',
  INFO: 'bg-blue-600',
  DEBUG: 'bg-gray-600'
}

const levelMap: Record<number, string> = {
  60: 'ERROR',
  50: 'ERROR',
  40: 'WARN',
  30: 'INFO',
  20: 'DEBUG',
  10: 'DEBUG'
}

const getLevelLabel = (level: number): string => levelMap[level] || 'INFO'

const getLevelClass = (log: LogEntry): string => {
  const level = getLevelLabel(log.level)
  const classes: Record<string, string> = {
    ERROR: 'bg-red-50 border-red-500',
    WARN: 'bg-yellow-50 border-yellow-500',
    INFO: 'bg-blue-50 border-blue-500',
    DEBUG: 'bg-gray-50 border-gray-400'
  }
  return classes[level] || 'bg-gray-50 border-gray-300'
}

const getLevelTextClass = (level: number): string => {
  const levelStr = getLevelLabel(level)
  const classes: Record<string, string> = {
    ERROR: 'text-red-600 font-bold',
    WARN: 'text-yellow-600 font-semibold',
    INFO: 'text-blue-600',
    DEBUG: 'text-gray-600'
  }
  return classes[levelStr] || 'text-gray-600'
}

const formatTime = (timestamp: number): string => {
  const date = new Date(timestamp)
  return date.toLocaleTimeString('en-US', { hour12: false })
}

const highlightText = (text: string): string => {
  if (!searchFilter.value) return text
  const regex = new RegExp(`(${searchFilter.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
  return text.replace(regex, '<mark class="bg-yellow-200">$1</mark>')
}

const toggleLevel = (level: string) => {
  const index = levelFilter.value.indexOf(level)
  if (index > -1) {
    levelFilter.value.splice(index, 1)
  } else {
    levelFilter.value.push(level)
  }
}

// Get all unique context IDs from logs
const allContextIds = computed(() => {
  const contexts = new Set<string>()
  logs.value.forEach((log) => {
    if (log.context) {
      contexts.add(log.context)
    }
  })
  return Array.from(contexts).sort((a, b) => {
    const orderA = getSectionSettings(a).order
    const orderB = getSectionSettings(b).order
    return orderA - orderB
  })
})

// Get visible contexts sorted by order
const visibleContextsSorted = computed(() => {
  return allContextIds.value.filter((context) => isVisible(context))
})

const isVisible = (contextId: string): boolean => {
  return getSectionSettings(contextId).visible
}

const getContextOrder = (contextId: string): number => {
  return allContextIds.value.indexOf(contextId)
}

const toggleVisibility = (contextId: string) => {
  toggleSectionVisibility(contextId)
}

const moveUp = (contextId: string) => {
  const currentIndex = allContextIds.value.indexOf(contextId)
  if (currentIndex > 0) {
    const reordered = [...allContextIds.value]
    ;[reordered[currentIndex], reordered[currentIndex - 1]] = [
      reordered[currentIndex - 1],
      reordered[currentIndex]
    ]
    reorderSections(currentIndex, currentIndex - 1, reordered)
  }
}

const moveDown = (contextId: string) => {
  const currentIndex = allContextIds.value.indexOf(contextId)
  if (currentIndex < allContextIds.value.length - 1) {
    const reordered = [...allContextIds.value]
    ;[reordered[currentIndex], reordered[currentIndex + 1]] = [
      reordered[currentIndex + 1],
      reordered[currentIndex]
    ]
    reorderSections(currentIndex, currentIndex + 1, reordered)
  }
}

// Filter logs by context
const getContextLogs = (context: string) => {
  return logs.value.filter((log) => {
    if (log.context !== context) return false

    // Apply level filter
    const level = getLevelLabel(log.level)
    if (!levelFilter.value.includes(level)) return false

    // Apply search filter
    if (searchFilter.value && !log.msg.toLowerCase().includes(searchFilter.value.toLowerCase())) {
      return false
    }

    return true
  })
}

const getContextLogCount = (context: string): number => {
  return getContextLogs(context).length
}

const fetchLogs = async () => {
  try {
    // Fetch logs from the worker log API endpoint
    const response = await fetch(`${API}/api/files/logs/worker?tail=1000`)

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }

    // Parse NDJSON (newline-delimited JSON)
    const text = await response.text()
    const lines = text.trim().split('\n')
    const parsed: LogEntry[] = []

    for (const line of lines) {
      if (line.trim()) {
        try {
          parsed.push(JSON.parse(line))
        } catch (e) {
          console.error('Failed to parse log line:', line, e)
        }
      }
    }

    logs.value = parsed
  } catch (error) {
    console.error('Failed to fetch logs:', error)
  }
}

const setupAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
  }

  if (autoRefresh.value) {
    refreshTimer = setInterval(() => {
      fetchLogs()
    }, refreshInterval.value * 1000)
  }
}

watch([autoRefresh, refreshInterval], () => {
  setupAutoRefresh()
})

onMounted(() => {
  fetchLogs()
  setupAutoRefresh()
})

onBeforeUnmount(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
  }
})
</script>

<style scoped>
mark {
  background-color: #fef08a;
  padding: 0 0.25rem;
  border-radius: 0.25rem;
}

/* Custom scrollbar */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background-color: #f3f4f6;
}

::-webkit-scrollbar-thumb {
  background-color: #9ca3af;
  border-radius: 0.25rem;
}

::-webkit-scrollbar-thumb:hover {
  background-color: #6b7280;
}
</style>
