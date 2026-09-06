<script setup lang="ts">
import { ref, computed, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { client } from '@/libs/client'

interface Props {
  file: any
  initialContent: string
  initialSize: number
}

const props = defineProps<Props>()
const emit = defineEmits(['close', 'download'])

const route = useRoute()
const router = useRouter()

const fileContent = ref(props.initialContent)
const logLines = ref<string[]>([])
const filteredLogLines = ref<{ line: string; originalIndex: number }[]>([])
const searchFilter = ref((route.query.search as string) || '')
const levelFilter = ref<string[]>(
  route.query.levels
    ? (route.query.levels as string).split(',')
    : ['ERROR', 'WARN', 'INFO', 'DEBUG']
)
const autoRefresh = ref(true)
const refreshInterval = ref(3)
let refreshTimer: NodeJS.Timeout | null = null
const autoScroll = ref(true)
const logContainer = ref<HTMLElement | null>(null)
const lastFileSize = ref(props.initialSize)
const tailLines = ref(500) // Number of lines to fetch from the end
const useTailMode = ref(true) // Use tail mode by default for better performance
const splitView = ref(false) // Toggle split view for context
const selectedLineIndex = ref<number | null>(null) // Selected line for context view
const contextLines = ref(5) // Number of context lines to show above and below
const contextGroups = ref<
  Array<{ lines: Array<{ line: string; originalIndex: number; isMatch: boolean }> }>
>([])
const currentContextIndex = ref(0)

const getLogLevel = (line: string): string => {
  const upperLine = line.toUpperCase()
  if (upperLine.includes('ERROR') || upperLine.includes('[ERROR]') || upperLine.includes('ERR:'))
    return 'ERROR'
  if (upperLine.includes('WARN') || upperLine.includes('[WARN]') || upperLine.includes('WARNING'))
    return 'WARN'
  if (upperLine.includes('INFO') || upperLine.includes('[INFO]')) return 'INFO'
  if (upperLine.includes('DEBUG') || upperLine.includes('[DEBUG]')) return 'DEBUG'
  return 'INFO'
}

const getLogLevelColor = (line: string): string => {
  const level = getLogLevel(line)
  const colors: Record<string, string> = {
    ERROR: 'error',
    WARN: 'warning',
    INFO: 'info',
    DEBUG: 'grey'
  }
  return colors[level] || 'default'
}

const highlightLine = (line: string): string => {
  if (!searchFilter.value) return line
  const regex = new RegExp(`(${searchFilter.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
  return line.replace(regex, '<mark>$1</mark>')
}

const getContextLines = (lineIndex: number) => {
  const start = Math.max(0, lineIndex - contextLines.value)
  const end = Math.min(logLines.value.length, lineIndex + contextLines.value + 1)
  return logLines.value.slice(start, end).map((line, idx) => ({
    line,
    lineNumber: start + idx + 1,
    isSelected: start + idx === lineIndex
  }))
}

const selectLine = (originalIndex: number) => {
  selectedLineIndex.value = originalIndex
  splitView.value = true // Auto-enable split view when selecting a line
}

const nextContext = () => {
  if (currentContextIndex.value < contextGroups.value.length - 1) {
    currentContextIndex.value++
  }
}

const prevContext = () => {
  if (currentContextIndex.value > 0) {
    currentContextIndex.value--
  }
}

const currentContext = computed(() => {
  return contextGroups.value[currentContextIndex.value] || null
})

const applyFilters = () => {
  // Create array with original indices
  let filtered = logLines.value.map((line, index) => ({ line, originalIndex: index }))

  // Filter by level
  if (levelFilter.value.length > 0 && levelFilter.value.length < 4) {
    filtered = filtered.filter((item) => {
      const level = getLogLevel(item.line)
      return levelFilter.value.includes(level)
    })
  }

  // Create context groups if search filter is active
  if (searchFilter.value) {
    const searchLower = searchFilter.value.toLowerCase()
    const matchedRanges: Array<{ start: number; end: number; matchIndex: number }> = []

    // Find all matching lines and calculate their context ranges
    logLines.value.forEach((line, originalIndex) => {
      const level = getLogLevel(line)
      if (
        levelFilter.value.length > 0 &&
        levelFilter.value.length < 4 &&
        !levelFilter.value.includes(level)
      ) {
        return
      }

      if (line.toLowerCase().includes(searchLower)) {
        const contextStart = Math.max(0, originalIndex - contextLines.value)
        const contextEnd = Math.min(logLines.value.length, originalIndex + contextLines.value + 1)
        matchedRanges.push({ start: contextStart, end: contextEnd, matchIndex: originalIndex })
      }
    })

    // Merge overlapping ranges
    const mergedRanges: Array<{ start: number; end: number; matchIndices: number[] }> = []
    matchedRanges.sort((a, b) => a.start - b.start)

    for (const range of matchedRanges) {
      if (mergedRanges.length === 0) {
        mergedRanges.push({ start: range.start, end: range.end, matchIndices: [range.matchIndex] })
      } else {
        const lastMerged = mergedRanges[mergedRanges.length - 1]
        // If ranges overlap or are adjacent, merge them
        if (range.start <= lastMerged.end) {
          lastMerged.end = Math.max(lastMerged.end, range.end)
          lastMerged.matchIndices.push(range.matchIndex)
        } else {
          mergedRanges.push({
            start: range.start,
            end: range.end,
            matchIndices: [range.matchIndex]
          })
        }
      }
    }

    // Create context groups from merged ranges
    const groups: Array<{
      lines: Array<{ line: string; originalIndex: number; isMatch: boolean }>
    }> = []

    for (const range of mergedRanges) {
      const contextLinesData: Array<{ line: string; originalIndex: number; isMatch: boolean }> = []

      for (let i = range.start; i < range.end; i++) {
        const contextLine = logLines.value[i]
        const contextLevel = getLogLevel(contextLine)

        // Apply level filter to context lines too
        if (
          levelFilter.value.length > 0 &&
          levelFilter.value.length < 4 &&
          !levelFilter.value.includes(contextLevel)
        ) {
          continue
        }

        contextLinesData.push({
          line: contextLine,
          originalIndex: i,
          isMatch: range.matchIndices.includes(i)
        })
      }

      if (contextLinesData.length > 0) {
        groups.push({ lines: contextLinesData })
      }
    }

    contextGroups.value = groups
    filteredLogLines.value = [] // Not used in context mode

    // Reset to first context if current index is out of bounds
    if (currentContextIndex.value >= groups.length) {
      currentContextIndex.value = 0
    }
  } else {
    // No search filter - show all filtered lines normally
    filteredLogLines.value = filtered
    contextGroups.value = []
    currentContextIndex.value = 0
  }

  // Auto scroll to bottom if enabled
  if (autoScroll.value) {
    setTimeout(() => {
      if (logContainer.value) {
        logContainer.value.scrollTop = logContainer.value.scrollHeight
      }
    }, 100)
  }
}

const parseLogContent = (content: string) => {
  logLines.value = content.split('\n')
  applyFilters()
}

const updateQueryParams = () => {
  const query = { ...route.query }

  if (searchFilter.value) {
    query.search = searchFilter.value
  } else {
    delete query.search
  }

  if (levelFilter.value.length > 0 && levelFilter.value.length < 4) {
    query.levels = levelFilter.value.join(',')
  } else {
    delete query.levels
  }

  router.push({ query })
}

const fetchLogUpdates = async () => {
  try {
    const params: any = { path: props.file.path }

    // Use tail mode to fetch only last N lines for better performance
    if (useTailMode.value) {
      params.tail = tailLines.value
    }

    const response = await client.get('/files/view', {
      params
    })

    if (!response.data.error) {
      const newContent = response.data.content || ''
      const newSize = response.data.size || 0

      // Only update if file has grown or in tail mode
      if (useTailMode.value || newSize > lastFileSize.value) {
        const wasAtBottom =
          autoScroll.value ||
          (logContainer.value &&
            logContainer.value.scrollHeight -
              logContainer.value.scrollTop -
              logContainer.value.clientHeight <
              100)

        fileContent.value = newContent
        parseLogContent(newContent)
        lastFileSize.value = newSize

        // Maintain scroll position if user was at bottom
        if (wasAtBottom && logContainer.value) {
          setTimeout(() => {
            if (logContainer.value) {
              logContainer.value.scrollTop = logContainer.value.scrollHeight
            }
          }, 50)
        }
      }
    }
  } catch (err) {
    console.error('Failed to fetch log updates:', err)
  }
}

const startAutoRefresh = () => {
  stopAutoRefresh()
  if (autoRefresh.value) {
    refreshTimer = setInterval(fetchLogUpdates, refreshInterval.value * 1000)
  }
}

const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

// Initialize
parseLogContent(props.initialContent)

// Start auto-refresh on mount if enabled
if (autoRefresh.value) {
  startAutoRefresh()
}

onBeforeUnmount(() => {
  stopAutoRefresh()
})

// Watch for filter changes
watch([searchFilter, levelFilter], () => {
  applyFilters()
  updateQueryParams()
})

// Watch for auto-refresh toggle
watch(autoRefresh, (newVal) => {
  if (newVal) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
})

// Watch for refresh interval changes
watch(refreshInterval, () => {
  if (autoRefresh.value) {
    startAutoRefresh()
  }
})

// Watch for tail mode or tail lines changes
watch([useTailMode, tailLines], () => {
  if (useTailMode.value) {
    fetchLogUpdates()
  }
})
</script>

<template>
  <div>
    <!-- Log Viewer Controls -->
    <v-card-text class="pb-2">
      <v-row dense>
        <v-col cols="12" md="3">
          <v-text-field
            v-model="searchFilter"
            label="Search logs..."
            placeholder="Filter by text"
            variant="outlined"
            density="compact"
            clearable
            hide-details
          >
            <template #prepend-inner>
              <v-icon size="small">mdi-magnify</v-icon>
            </template>
          </v-text-field>
        </v-col>
        <v-col cols="12" md="3">
          <v-select
            v-model="levelFilter"
            :items="['ERROR', 'WARN', 'INFO', 'DEBUG']"
            label="Log Levels"
            variant="outlined"
            density="compact"
            multiple
            chips
            hide-details
          >
            <template #prepend-inner>
              <v-icon size="small">mdi-filter</v-icon>
            </template>
          </v-select>
        </v-col>
        <v-col cols="12" md="3">
          <div class="d-flex align-center">
            <v-switch
              v-model="useTailMode"
              label="Tail Mode"
              color="success"
              density="compact"
              hide-details
              class="flex-grow-1"
            ></v-switch>
            <v-select
              v-model="tailLines"
              :items="[100, 200, 500, 1000, 2000]"
              label="Lines"
              variant="outlined"
              density="compact"
              :disabled="!useTailMode"
              hide-details
              class="ml-2"
              style="max-width: 100px"
            ></v-select>
          </div>
        </v-col>
        <v-col cols="12" md="3">
          <v-row dense class="ma-0">
            <v-col cols="12" sm="4" class="pa-1">
              <div class="d-flex align-center">
                <v-switch
                  v-model="autoRefresh"
                  label="Auto-refresh"
                  color="primary"
                  density="compact"
                  hide-details
                  class="flex-grow-1"
                ></v-switch>
                <v-select
                  v-model="refreshInterval"
                  :items="[1, 2, 3, 5, 10]"
                  label="Sec"
                  variant="outlined"
                  density="compact"
                  :disabled="!autoRefresh"
                  hide-details
                  class="ml-2"
                  style="max-width: 80px"
                ></v-select>
              </div>
            </v-col>
            <v-col cols="12" sm="4" class="pa-1">
              <v-switch
                v-model="autoScroll"
                label="Auto-scroll"
                color="primary"
                density="compact"
                hide-details
              ></v-switch>
            </v-col>
            <v-col cols="12" sm="4" class="pa-1">
              <v-switch
                v-model="splitView"
                label="Split View"
                color="secondary"
                density="compact"
                hide-details
              ></v-switch>
            </v-col>
          </v-row>
        </v-col>
      </v-row>
      <v-row dense class="mt-2">
        <v-col cols="12">
          <v-chip v-if="contextGroups.length > 0" size="small" variant="text" color="primary">
            <v-icon size="small" class="mr-1">mdi-file-document-multiple</v-icon>
            {{ contextGroups.length }} context{{ contextGroups.length !== 1 ? 's' : '' }}
          </v-chip>
          <v-chip v-else size="small" variant="text">
            <v-icon size="small" class="mr-1">mdi-text-box-multiple</v-icon>
            {{ filteredLogLines.length }} / {{ logLines.length }} lines
          </v-chip>
          <v-chip v-if="useTailMode" size="small" variant="text" color="success" class="ml-2">
            <v-icon size="small" class="mr-1">mdi-arrow-down-bold</v-icon>
            Showing last {{ tailLines }} lines
          </v-chip>
        </v-col>
      </v-row>
    </v-card-text>

    <v-divider></v-divider>

    <!-- Log Content -->
    <v-card-text class="pa-0">
      <!-- Context View Mode (when search is active) -->
      <div v-if="contextGroups.length > 0" class="contexts-wrapper">
        <div v-for="(group, groupIndex) in contextGroups" :key="groupIndex" class="context-section">
          <!-- Context Header -->
          <div class="context-header">
            <v-icon size="small" class="mr-2">mdi-file-document-outline</v-icon>
            Context {{ groupIndex + 1 }}
            <span class="context-line-range ml-2">
              (Lines {{ group.lines[0].originalIndex + 1 }} -
              {{ group.lines[group.lines.length - 1].originalIndex + 1 }})
            </span>
          </div>

          <!-- Context Logs -->
          <div class="log-viewer context-log-viewer">
            <div
              v-for="(item, index) in group.lines"
              :key="index"
              :class="[
                'log-line',
                `log-${getLogLevelColor(item.line)}`,
                { 'log-line-match': item.isMatch }
              ]"
            >
              <span class="log-line-number">{{ item.originalIndex + 1 }}</span>
              <span
                class="log-line-content"
                v-html="item.isMatch ? highlightLine(item.line) : item.line"
              ></span>
            </div>
          </div>
        </div>
      </div>

      <!-- Normal View Mode (no search filter) -->
      <div v-else ref="logContainer" class="log-viewer">
        <div
          v-for="(item, index) in filteredLogLines"
          :key="index"
          :class="['log-line', `log-${getLogLevelColor(item.line)}`]"
          @click="selectLine(item.originalIndex)"
          style="cursor: pointer"
        >
          <span class="log-line-number">{{ item.originalIndex + 1 }}</span>
          <span class="log-line-content" v-html="highlightLine(item.line)"></span>
        </div>
        <div v-if="filteredLogLines.length === 0" class="text-center pa-8">
          <v-icon size="48" color="grey-lighten-1">mdi-filter-off</v-icon>
          <div class="text-body-1 mt-2 text-grey">No matching log lines</div>
          <div class="text-caption text-grey">Try adjusting your filters</div>
        </div>
      </div>
    </v-card-text>
  </div>
</template>

<style scoped>
.log-viewer {
  background-color: #1e1e1e;
  color: #d4d4d4;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  max-height: 600px;
  overflow-y: auto;
  overflow-x: auto;
  padding: 8px 0;
}

@media (max-width: 600px) {
  .log-viewer {
    font-size: 11px;
    max-height: 400px;
  }
}

.log-line {
  display: flex;
  padding: 2px 12px;
  border-left: 3px solid transparent;
  transition: background-color 0.1s ease;
}

@media (max-width: 600px) {
  .log-line {
    padding: 2px 8px;
    font-size: 11px;
  }
}

.log-line:hover {
  background-color: rgba(255, 255, 255, 0.05);
}

.log-line-number {
  flex-shrink: 0;
  width: 50px;
  color: #858585;
  text-align: right;
  margin-right: 16px;
  user-select: none;
}

@media (max-width: 600px) {
  .log-line-number {
    width: 35px;
    margin-right: 8px;
    font-size: 10px;
  }
}

.log-line-content {
  flex: 1;
  white-space: pre-wrap;
  word-break: break-all;
}

.log-error {
  border-left-color: #f44336;
  background-color: rgba(244, 67, 54, 0.05);
}

.log-error .log-line-content {
  color: #ff6b6b;
}

.log-warning {
  border-left-color: #ff9800;
  background-color: rgba(255, 152, 0, 0.05);
}

.log-warning .log-line-content {
  color: #ffb74d;
}

.log-info {
  border-left-color: #2196f3;
}

.log-info .log-line-content {
  color: #64b5f6;
}

.log-grey .log-line-content {
  color: #9e9e9e;
}

.log-default {
  border-left-color: transparent;
}

:deep(mark) {
  background-color: #ffd54f;
  color: #000;
  padding: 2px 4px;
  border-radius: 2px;
  font-weight: bold;
}

.contexts-wrapper {
  max-height: 600px;
  overflow-y: auto;
  background-color: #1e1e1e;
  padding: 16px;
}

@media (max-width: 600px) {
  .contexts-wrapper {
    max-height: 400px;
    padding: 8px;
  }
}

.context-section {
  background-color: #252525;
  border: 2px solid #3a3a3a;
  border-radius: 8px;
  margin-bottom: 24px;
  overflow: hidden;
  box-shadow: 0 4px 6px rgba(0, 0, 0, 0.3);
}

.context-section:last-child {
  margin-bottom: 0;
}

.context-header {
  background: linear-gradient(135deg, #3a3a3a 0%, #2d2d2d 100%);
  color: #ffffff;
  padding: 12px 16px;
  font-weight: 700;
  font-size: 14px;
  border-bottom: 3px solid #ffd54f;
  display: flex;
  align-items: center;
  letter-spacing: 0.5px;
}

.context-line-range {
  color: #b0b0b0;
  font-weight: 500;
  font-size: 12px;
}

.context-log-viewer {
  max-height: none !important;
  padding: 8px 0;
  background-color: #1e1e1e;
}

.log-line-match {
  background-color: rgba(255, 213, 79, 0.25) !important;
  border-left-color: #ffd54f !important;
  border-left-width: 5px !important;
  font-weight: 600;
}
</style>
