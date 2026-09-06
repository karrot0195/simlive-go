<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { client } from '@/libs/client'
import LogViewer from '@/components/LogViewer.vue'
import FileViewer from '@/components/FileViewer.vue'

interface BreadcrumbItem {
  title: string
  path: string
  disabled?: boolean
}

const route = useRoute()
const router = useRouter()

const currentFolder = ref<'data' | 'logs'>('data')
const currentPath = ref<string>('')
const files = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const selectedFile = ref<any>(null)
const fileContent = ref('')
const viewingFile = ref(false)
const downloadingFile = ref(false)
const isLogFile = ref(false)
const fileSize = ref(0)
const fileSearchFilter = ref('')

const folders = [
  { value: 'data', title: 'Data', icon: 'mdi-database' },
  { value: 'logs', title: 'Logs', icon: 'mdi-text-box' }
]

// Breadcrumbs for navigation
const breadcrumbs = computed(() => {
  const crumbs: BreadcrumbItem[] = [{ title: currentFolder.value, path: '' }]

  if (currentPath.value) {
    const parts = currentPath.value.split('/').filter((p) => p)
    let accPath = ''
    for (const part of parts) {
      accPath += (accPath ? '/' : '') + part
      crumbs.push({ title: part, path: accPath })
    }
  }

  return crumbs
})

const filteredFiles = computed(() => {
  if (!fileSearchFilter.value.trim()) {
    return files.value
  }
  const searchTerm = fileSearchFilter.value.toLowerCase()
  return files.value.filter((file) => file.name.toLowerCase().includes(searchTerm))
})

const formatSize = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const formatDate = (dateStr: string) => {
  const date = new Date(dateStr)
  return date.toLocaleString()
}

const fetchFiles = async (path?: string) => {
  loading.value = true
  error.value = ''
  viewingFile.value = false
  selectedFile.value = null

  const targetPath = path !== undefined ? path : currentPath.value
  const fullPath = targetPath ? `${currentFolder.value}/${targetPath}` : currentFolder.value

  try {
    const response = await client.get('/files/list', {
      params: { dir: fullPath }
    })

    if (response.data.error) {
      error.value = response.data.error
      files.value = []
    } else {
      files.value = response.data.files || []

      // Extract relative path within the current folder
      const returnedPath = response.data.currentPath || ''
      const folderPrefix = currentFolder.value + '/'
      if (returnedPath.startsWith(folderPrefix)) {
        currentPath.value = returnedPath.substring(folderPrefix.length)
      } else if (returnedPath === currentFolder.value) {
        currentPath.value = ''
      } else {
        currentPath.value = returnedPath
      }

      // Update URL
      router.push({
        query: {
          ...route.query, // Preserve existing query params (search, levels, etc.)
          folder: currentFolder.value,
          path: currentPath.value || undefined,
          filename: fileSearchFilter.value || undefined
        }
      })
    }
  } catch (err: any) {
    error.value = err.response?.data?.message || 'Failed to fetch files'
    files.value = []
  } finally {
    loading.value = false
  }
}

const navigateToDirectory = (file: any) => {
  if (file.isDirectory) {
    // Remove folder prefix from path
    const folderPrefix = currentFolder.value + '/'
    let relativePath = file.path
    if (relativePath.startsWith(folderPrefix)) {
      relativePath = relativePath.substring(folderPrefix.length)
    }
    fetchFiles(relativePath)
  }
}

const navigateToBreadcrumb = (path: string) => {
  fetchFiles(path)
}

const downloadFile = async (file: any) => {
  if (file.isDirectory) return

  downloadingFile.value = true

  try {
    const fullPath = file.path.startsWith(currentFolder.value + '/')
      ? file.path
      : `${currentFolder.value}/${file.path}`
    const response = await client.get('/files/download', {
      params: { path: fullPath },
      responseType: 'blob'
    })

    // Create download link
    const url = window.URL.createObjectURL(new Blob([response.data]))
    const link = document.createElement('a')
    link.href = url
    link.setAttribute('download', file.name)
    document.body.appendChild(link)
    link.click()
    link.remove()
    window.URL.revokeObjectURL(url)
  } catch (err: any) {
    error.value = err.response?.data?.message || 'Failed to download file'
  } finally {
    downloadingFile.value = false
  }
}

const closeFileView = () => {
  viewingFile.value = false
  selectedFile.value = null
  fileContent.value = ''
  isLogFile.value = false

  // Remove viewFile and log-related filters from query params
  const query = { ...route.query }
  delete query.viewFile
  delete query.search
  delete query.levels
  router.push({ query })
}

const goBack = () => {
  const parts = currentPath.value.split('/').filter((p) => p)
  parts.pop()
  const newPath = parts.length > 0 ? parts.join('/') : ''
  fetchFiles(newPath)
}

const switchFolder = (folder: 'data' | 'logs') => {
  currentFolder.value = folder
  currentPath.value = ''
  fetchFiles()
}

const getFileIcon = (file: any) => {
  if (file.isDirectory) return 'mdi-folder'

  const ext = file.name.split('.').pop()?.toLowerCase()
  const iconMap: Record<string, string> = {
    txt: 'mdi-file-document-outline',
    json: 'mdi-code-json',
    xml: 'mdi-file-code',
    log: 'mdi-text-box',
    csv: 'mdi-file-delimited',
    md: 'mdi-language-markdown',
    ts: 'mdi-language-typescript',
    js: 'mdi-language-javascript',
    vue: 'mdi-vuejs'
  }

  return iconMap[ext || ''] || 'mdi-file'
}

const getFileColor = (file: any) => {
  if (file.isDirectory) return 'amber'
  return 'blue-grey'
}

const isLog = (fileName: string) => {
  const ext = fileName.split('.').pop()?.toLowerCase()
  return ext === 'log' || fileName.includes('.log')
}

const viewFile = async (file: any) => {
  if (file.isDirectory) return

  loading.value = true
  error.value = ''

  try {
    const fullPath = file.path.startsWith(currentFolder.value + '/')
      ? file.path
      : `${currentFolder.value}/${file.path}`
    const response = await client.get('/files/view', {
      params: { path: fullPath }
    })

    if (response.data.error) {
      error.value = response.data.error
    } else {
      selectedFile.value = file
      fileContent.value = response.data.content || ''
      fileSize.value = response.data.size || 0
      isLogFile.value = isLog(file.name)
      viewingFile.value = true

      // Update query params to include viewing state
      router.push({
        query: {
          ...route.query,
          folder: currentFolder.value,
          path: currentPath.value || undefined,
          viewFile: file.name
        }
      })
    }
  } catch (err: any) {
    error.value = err.response?.data?.message || 'Failed to view file'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  // Check if there's a folder and path in query params
  const queryFolder = route.query.folder as string
  if (queryFolder === 'data' || queryFolder === 'logs') {
    currentFolder.value = queryFolder
  }

  const queryPath = route.query.path as string
  if (queryPath) {
    currentPath.value = queryPath
  }

  const queryFilename = route.query.filename as string
  if (queryFilename) {
    fileSearchFilter.value = queryFilename
  }

  await fetchFiles()

  // If viewFile is in query params, open that file
  const queryViewFile = route.query.viewFile as string
  if (queryViewFile) {
    const file = files.value.find((f) => f.name === queryViewFile)
    if (file) {
      await viewFile(file)
    }
  }
})

// Watch for file search filter changes and update query params
watch(fileSearchFilter, (newValue) => {
  const query = { ...route.query }
  if (newValue) {
    query.filename = newValue
  } else {
    delete query.filename
  }
  router.push({ query })
})
</script>

<template>
  <v-container fluid class="pa-6">
    <v-card>
      <v-card-title class="bg-primary">
        <v-icon class="mr-2">mdi-folder-open</v-icon>
        File Browser - /tmp/data
      </v-card-title>

      <v-card-text class="pa-6">
        <!-- Folder Selector -->
        <v-row class="mb-4">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-text class="pa-2 pa-sm-4">
                <v-btn-toggle
                  v-model="currentFolder"
                  color="primary"
                  mandatory
                  variant="outlined"
                  divided
                  class="w-100"
                >
                  <v-btn
                    v-for="folder in folders"
                    :key="folder.value"
                    :value="folder.value"
                    @click="switchFolder(folder.value as 'data' | 'logs')"
                    class="flex-grow-1"
                  >
                    <v-icon
                      class="mr-1 mr-sm-2"
                      :size="$vuetify.display.xs ? 'small' : 'default'"
                      >{{ folder.icon }}</v-icon
                    >
                    <span :class="$vuetify.display.xs ? 'text-caption' : ''">{{
                      folder.title
                    }}</span>
                  </v-btn>
                </v-btn-toggle>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <!-- Breadcrumb Navigation -->
        <v-row class="mb-4">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-text class="d-flex align-center pa-2 pa-sm-4">
                <v-btn
                  v-if="currentPath"
                  icon="mdi-arrow-left"
                  size="small"
                  variant="text"
                  @click="goBack"
                  class="mr-2"
                ></v-btn>
                <v-breadcrumbs :items="breadcrumbs" class="pa-0 flex-grow-1" density="compact">
                  <template #item="{ item }">
                    <v-breadcrumbs-item
                      @click="navigateToBreadcrumb((item as BreadcrumbItem).path)"
                      :disabled="(item as BreadcrumbItem).path === currentPath"
                      class="cursor-pointer text-truncate"
                      style="max-width: 150px"
                    >
                      {{ (item as BreadcrumbItem).title }}
                    </v-breadcrumbs-item>
                  </template>
                  <template #divider>
                    <v-icon size="small">mdi-chevron-right</v-icon>
                  </template>
                </v-breadcrumbs>
                <v-btn
                  color="primary"
                  @click="fetchFiles()"
                  :loading="loading"
                  size="small"
                  :icon="$vuetify.display.xs"
                  class="ml-2 flex-shrink-0"
                >
                  <v-icon :class="$vuetify.display.xs ? '' : 'mr-2'">mdi-refresh</v-icon>
                  <span v-if="!$vuetify.display.xs">Refresh</span>
                </v-btn>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <!-- Error Alert -->
        <v-alert v-if="error" type="error" variant="tonal" class="mb-4">
          <v-icon class="mr-2">mdi-alert-circle</v-icon>
          {{ error }}
        </v-alert>

        <!-- File List -->
        <v-row v-if="!viewingFile">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-title class="d-flex justify-space-between align-center flex-wrap">
                <div class="d-flex align-center">
                  <v-icon class="mr-2" size="small">mdi-file-document-multiple</v-icon>
                  <span
                    >Files ({{ filteredFiles.length
                    }}{{ fileSearchFilter ? ` / ${files.length}` : '' }})</span
                  >
                </div>
                <v-text-field
                  v-model="fileSearchFilter"
                  placeholder="Search files..."
                  variant="outlined"
                  density="compact"
                  clearable
                  hide-details
                  style="max-width: 300px"
                  class="mt-2 mt-sm-0"
                >
                  <template #prepend-inner>
                    <v-icon size="small">mdi-magnify</v-icon>
                  </template>
                </v-text-field>
              </v-card-title>

              <v-divider></v-divider>

              <v-list v-if="filteredFiles.length > 0 && !loading" density="compact">
                <v-list-item
                  v-for="file in filteredFiles"
                  :key="file.path"
                  @click="file.isDirectory ? navigateToDirectory(file) : viewFile(file)"
                  class="cursor-pointer"
                >
                  <template #prepend>
                    <v-icon
                      :color="getFileColor(file)"
                      :size="$vuetify.display.xs ? 'default' : 'large'"
                    >
                      {{ getFileIcon(file) }}
                    </v-icon>
                  </template>

                  <v-list-item-title :class="$vuetify.display.xs ? 'text-body-2' : ''">
                    {{ file.name }}
                  </v-list-item-title>

                  <v-list-item-subtitle :class="$vuetify.display.xs ? 'text-caption' : ''">
                    <div class="d-flex align-center flex-wrap">
                      <span class="mr-2 mr-sm-4">{{ formatSize(file.size) }}</span>
                      <span v-if="!$vuetify.display.xs"
                        >Modified: {{ formatDate(file.modified) }}</span
                      >
                    </div>
                  </v-list-item-subtitle>

                  <template #append>
                    <div class="d-flex">
                      <v-btn
                        v-if="!file.isDirectory"
                        icon="mdi-eye"
                        size="small"
                        variant="text"
                        @click.stop="viewFile(file)"
                        title="View file"
                      ></v-btn>
                      <v-btn
                        v-if="!file.isDirectory"
                        icon="mdi-download"
                        size="small"
                        variant="text"
                        @click.stop="downloadFile(file)"
                        title="Download file"
                        :loading="downloadingFile"
                      ></v-btn>
                    </div>
                  </template>
                </v-list-item>
              </v-list>

              <div v-else-if="loading" class="text-center pa-8">
                <v-progress-circular indeterminate color="primary"></v-progress-circular>
                <div class="text-body-2 mt-4 text-grey">Loading files...</div>
              </div>

              <div v-else-if="!loading" class="text-center pa-8">
                <v-icon size="64" color="grey-lighten-1">
                  {{ fileSearchFilter ? 'mdi-file-search' : 'mdi-folder-open' }}
                </v-icon>
                <div class="text-h6 mt-4 text-grey">
                  {{ fileSearchFilter ? 'No Matching Files' : 'No Files' }}
                </div>
                <div class="text-body-2 text-grey">
                  {{ fileSearchFilter ? 'Try a different search term' : 'This directory is empty' }}
                </div>
              </div>
            </v-card>
          </v-col>
        </v-row>

        <!-- File/Log Viewer -->
        <v-row v-if="viewingFile && selectedFile">
          <v-col cols="12">
            <v-card variant="outlined">
              <v-card-title
                class="d-flex justify-space-between align-center pa-3 pa-sm-4 flex-wrap"
              >
                <div class="d-flex align-center mb-2 mb-sm-0">
                  <v-icon class="mr-2" :size="$vuetify.display.xs ? 'small' : 'default'">{{
                    getFileIcon(selectedFile)
                  }}</v-icon>
                  <span
                    :class="$vuetify.display.xs ? 'text-body-2' : 'text-subtitle-1'"
                    class="text-truncate"
                    style="max-width: 200px"
                  >
                    {{ selectedFile.name }}
                  </span>
                  <v-chip v-if="isLogFile" size="small" color="info" class="ml-2">
                    <v-icon size="small" class="mr-1">mdi-text-box</v-icon>
                    <span v-if="!$vuetify.display.xs">Log File</span>
                  </v-chip>
                </div>
                <div class="d-flex align-center">
                  <v-chip size="small" class="mr-2">
                    {{ formatSize(selectedFile.size) }}
                  </v-chip>
                  <v-btn
                    icon="mdi-download"
                    size="small"
                    variant="text"
                    @click="downloadFile(selectedFile)"
                    title="Download"
                  ></v-btn>
                  <v-btn
                    icon="mdi-close"
                    size="small"
                    variant="text"
                    @click="closeFileView"
                    title="Close"
                  ></v-btn>
                </div>
              </v-card-title>

              <v-divider></v-divider>

              <!-- Log Viewer (for log files) -->
              <LogViewer
                v-if="isLogFile"
                :file="selectedFile"
                :initial-content="fileContent"
                :initial-size="fileSize"
              />

              <!-- Regular File Viewer (for non-log files) -->
              <FileViewer v-else :content="fileContent" />
            </v-card>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>
  </v-container>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}

.cursor-pointer:hover {
  background-color: rgba(0, 0, 0, 0.04);
}
</style>
