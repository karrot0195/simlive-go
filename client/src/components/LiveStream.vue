<template>
  <div class="livestream-container">
    <div class="video-section">
      <div class="video-wrapper">
        <video
          ref="videoPlayer"
          id="videoPlayer"
          class="video-js vjs-default-skin"
          controls
          preload="auto"
          muted
        >
          <source :src="src" :type="type" />
          Your browser does not support the video tag.
        </video>

        <!-- Ad Break Overlay -->
        <div
          v-if="currentSegment && (currentSegment.cueOut || currentSegment.cueOutCont)"
          class="ad-overlay"
        >
          <div class="ad-label">
            <span class="ad-icon">📺</span>
            <span class="ad-text">ADS PRESENTING</span>
            <span class="ad-duration" v-if="currentSegment.cueOut">{{
              extractCueDuration(currentSegment.cueOut)
            }}</span>
            <span class="ad-elapsed" v-if="currentSegment.cueOutCont">{{
              extractCueElapsed(currentSegment.cueOutCont)
            }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="info-sidebar pt-8" v-if="currentSegment">
      <!-- M3U8 Playlist Info -->
      <div class="info-panel m3u8-info" v-if="playlistInfo">
        <div class="panel-header">
          <div class="panel-title">
            <span class="title-icon">📋</span>
            <strong>Playlist Info</strong>
          </div>
          <span v-if="isLivePlaylist" class="live-badge">🔴 LIVE</span>
          <span v-else class="vod-badge">📹 VOD</span>
        </div>
        <div class="panel-content">
          <div class="info-row" v-if="playlistInfo.version">
            <span class="label">Version:</span>
            <span class="value">{{ playlistInfo.version }}</span>
          </div>
          <div class="info-row" v-if="playlistInfo.targetDuration">
            <span class="label">Target Duration:</span>
            <span class="value">{{ playlistInfo.targetDuration }}s</span>
          </div>
          <div class="info-row" v-if="playlistInfo.mediaSequence !== undefined">
            <span class="label">Media Sequence:</span>
            <span class="value">{{ playlistInfo.mediaSequence }}</span>
          </div>
          <div class="info-row" v-if="playlistInfo.playlistType">
            <span class="label">Type:</span>
            <span class="value">{{ playlistInfo.playlistType }}</span>
          </div>
        </div>
      </div>

      <!-- Current Segment Info -->
      <div class="info-panel current-segment">
        <div class="panel-header">
          <div class="panel-title">
            <span class="title-icon">▶️</span>
            <strong>Loaded Segment</strong>
          </div>
        </div>
        <div class="panel-content">
          <div class="segment-uri-display">{{ currentSegment.uri }}</div>
          <div class="info-row">
            <span class="label">Duration:</span>
            <span class="value">{{ currentSegment.duration.toFixed(2) }}s</span>
          </div>
          <div class="info-row" v-if="currentSegment.programDateTime">
            <span class="label">Time:</span>
            <span class="value time">{{ formatDateTime(currentSegment.programDateTime) }}</span>
          </div>

          <!-- Cue Info -->
          <div v-if="currentSegment.cueOut" class="cue-info cue-out">
            <span class="cue-icon">🔴</span>
            <div class="cue-details">
              <div class="cue-label">CUE-OUT</div>
              <div class="cue-value">{{ extractCueDuration(currentSegment.cueOut) }}</div>
            </div>
          </div>
          <div v-if="currentSegment.cueIn" class="cue-info cue-in">
            <span class="cue-icon">🟢</span>
            <div class="cue-details">
              <div class="cue-label">CUE-IN</div>
              <div class="cue-value">Resuming content</div>
            </div>
          </div>
          <div v-if="currentSegment.cueOutCont" class="cue-info cue-cont">
            <span class="cue-icon">🟡</span>
            <div class="cue-details">
              <div class="cue-label">CUE-OUT-CONT</div>
              <div class="cue-value">{{ extractCueElapsed(currentSegment.cueOutCont) }}</div>
            </div>
          </div>

          <div class="info-row" v-if="currentSegment.discontinuity">
            <span class="label warning">⚠️ Discontinuity:</span>
            <span class="value">Yes</span>
          </div>
        </div>

        <div v-for="(tag, idx) in currentSegment.rawTags" :key="idx" class="raw-tag">
          {{ tag }}
        </div>
      </div>

      <!-- All Segments List -->
      <div class="info-panel segments-panel">
        <div class="panel-header">
          <div class="panel-title">
            <span class="title-icon">📊</span>
            <strong>Segments history ({{ allSegments.length }})</strong>
          </div>
        </div>
        <div class="segments-list-wrapper" ref="segmentsListWrapper">
          <div
            v-for="(segment, index) in displaySegments"
            :key="segment.uri"
            :class="['segment-card', { active: segment.uri === currentSegment?.uri }]"
            @click="selectSegment(segment)"
          >
            <div class="segment-header">
              <div class="segment-header-left">
                <span class="segment-number">#{{ index + 1 }}</span>
                <span class="segment-duration"
                  >{{ segment.duration ? segment.duration.toFixed(2) : '0.00' }}s</span
                >
              </div>
              <button
                v-if="isSuperAdmin"
                class="segment-nav-btn"
                @click.stop="navigateToTmpData(segment)"
                title="View in /tmp/data"
              >
                📁
              </button>
            </div>
            <div class="segment-body">
              <div class="segment-uri">{{ segment.uri }}</div>
              <div class="segment-metadata">
                <div v-if="segment.programDateTime" class="segment-time">
                  🕐 {{ formatDateTime(segment.programDateTime) }}
                </div>
                <div v-if="segment.byterange" class="segment-detail">
                  📦 {{ segment.byterange }}
                </div>
                <div v-if="segment.map" class="segment-detail">
                  🗺️ Map: {{ segment.map.uri ? segment.map.uri.slice(-20) : 'Present' }}
                </div>
              </div>
            </div>
            <div
              v-if="
                segment.dateRangeInfo ||
                segment.cueOut ||
                segment.cueIn ||
                segment.cueOutCont ||
                segment.discontinuity ||
                segment.key
              "
              class="segment-tags"
            >
              <!-- DATERANGE with detailed info -->
              <div v-if="segment.dateRangeInfo" class="tag-detailed daterange">
                <div class="tag-header">📺 AD BREAK</div>
                <div class="tag-body">
                  <div class="tag-item" v-if="segment.dateRangeInfo.id">
                    <span class="tag-label">ID:</span> {{ segment.dateRangeInfo.id }}
                  </div>
                  <div class="tag-item" v-if="segment.dateRangeInfo.duration">
                    <span class="tag-label">Duration:</span> {{ segment.dateRangeInfo.duration }}s
                  </div>
                  <div class="tag-item" v-if="segment.dateRangeInfo.scte35Out">
                    <span class="tag-label">SCTE35:</span>
                    <span class="scte35-preview"
                      >{{ segment.dateRangeInfo.scte35Out.slice(0, 40) }}...</span
                    >
                  </div>
                  <div class="tag-item" v-if="segment.dateRangeInfo.startDate">
                    <span class="tag-label">Start:</span>
                    {{ formatDateTime(segment.dateRangeInfo.startDate) }}
                  </div>
                </div>
              </div>

              <!-- Simple badges -->
              <div class="tag-badges">
                <span v-if="segment.cueOut" class="badge cue-out">
                  🔴 CUE-OUT {{ extractCueDuration(segment.cueOut) }}
                </span>
                <span v-if="segment.cueIn" class="badge cue-in">🟢 CUE-IN</span>
                <span v-if="segment.cueOutCont" class="badge cue-cont">
                  🟡 {{ extractCueElapsed(segment.cueOutCont) }}
                </span>
                <span v-if="segment.discontinuity" class="badge discontinuity">⚠️ DISC</span>
                <span v-if="segment.key" class="badge key">🔒 {{ segment.key.method }}</span>
                <span v-if="segment.map" class="badge map">🗺️ MAP</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import videojs from 'video.js'
import 'videojs-contrib-quality-menu'
import 'video.js/dist/video-js.css'

import { LIVE_API } from '@/constants'
import * as parser from 'm3u8-parser'

const props = defineProps({
  node: { type: Object, required: true },
  type: { type: String, required: true }
})

const router = useRouter()
const isSuperAdmin = computed(() => localStorage.getItem('is_super_admin') === 'true')

const src = computed(() => LIVE_API + '/' + props.node?.uuid + '/playout.m3u8')
const videoPlayer = ref(null)
let player = ref<any>(null)
const currentSegment = ref<any>(null)
const allSegments = ref<any[]>([])
const playlistInfo = ref<any>(null)
const rawM3u8Content = ref<string>('')
const selectedSegmentTags = ref<any>(null)
const mediaPlaylistUrl = ref<string>('')
const isLivePlaylist = ref<boolean>(false)
const maxSegmentsToKeep = 50 // Keep last 50 segments for live streams
const segmentsListWrapper = ref<HTMLElement | null>(null)
const seenSegmentUris = ref<Set<string>>(new Set())

// Computed property to display segments (already in newest-first order)
const displaySegments = computed(() => {
  return allSegments.value
})

const reloadVideo = () => {
  console.log('trigger reload')
  player.value.src(src.value)
  player.value.load()
  player.value.play()
}

const formatDateTime = (dateTime: string) => {
  if (!dateTime) return ''
  try {
    const date = new Date(dateTime)
    return date.toLocaleTimeString()
  } catch (e) {
    return dateTime
  }
}

const extractSegmentNumber = (uri: string) => {
  // Extract segment number from URI like /segments/400 or segments/400
  const match = uri.match(/segments\/(\d+)/)
  return match ? match[1] : null
}

const extractCueDuration = (cueOutTag: string) => {
  const match = cueOutTag.match(/DURATION=([\d.]+)/)
  return match ? `${match[1]}s` : 'Unknown'
}

const extractCueElapsed = (cueOutContTag: string) => {
  const elapsedMatch = cueOutContTag.match(/ElapsedTime=([\d.]+)/)
  const durationMatch = cueOutContTag.match(/Duration=([\d.]+)/)
  const elapsed = elapsedMatch ? elapsedMatch[1] : '?'
  const duration = durationMatch ? durationMatch[1] : '?'
  return `${elapsed}s / ${duration}s`
}

const navigateToTmpData = (segment: any) => {
  // Parse segment URI to extract filename
  // Expected format: L2/segments/337.ts or similar patterns
  const uri = segment.uri

  // Extract filename without extension (e.g., 337 from 337.ts)
  const filenameMatch = uri.match(/\/(\d+)(?:\.ts)?$/)

  if (filenameMatch && props.node?.uuid) {
    const filename = filenameMatch[1]
    const queryParams = new URLSearchParams({
      folder: 'data',
      path: props.node.uuid,
      filename: filename
    })

    window.open(`/tmp-data?${queryParams.toString()}`, '_blank')
  }
}

const fetchM3u8Content = async () => {
  try {
    let urlToFetch = src.value

    // If we already know the media playlist URL, use that
    if (mediaPlaylistUrl.value) {
      urlToFetch = mediaPlaylistUrl.value
    }

    console.log('Fetching m3u8 from:', urlToFetch)
    const response = await fetch(urlToFetch)
    const content = await response.text()
    console.log('M3U8 content length:', content.length)

    // Check if this is a master playlist (contains #EXT-X-STREAM-INF)
    if (content.includes('#EXT-X-STREAM-INF')) {
      console.log('This is a master playlist, finding media playlist...')
      const lines = content.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i].trim()
        if (!line.startsWith('#') && line.includes('.m3u8')) {
          // Found media playlist reference
          const baseUrl = urlToFetch.substring(0, urlToFetch.lastIndexOf('/') + 1)
          mediaPlaylistUrl.value = baseUrl + line
          console.log('Found media playlist:', mediaPlaylistUrl.value)
          // Fetch the actual media playlist
          await fetchM3u8Content()
          return
        }
      }
    } else {
      // This is a media playlist with segments
      rawM3u8Content.value = content
      parseM3u8Tags()
    }
  } catch (error) {
    console.error('Error fetching m3u8:', error)
  }
}

const parseM3u8Tags = () => {
  if (!rawM3u8Content.value) return

  // Use m3u8-parser library
  const m3u8Parser = new parser.Parser()
  m3u8Parser.push(rawM3u8Content.value)
  m3u8Parser.end()

  const parsedManifest = m3u8Parser.manifest
  console.log('Parsed manifest:', parsedManifest)

  // Check if this is a live playlist (no EXT-X-ENDLIST)
  isLivePlaylist.value = !parsedManifest.endList

  if (!parsedManifest.segments || parsedManifest.segments.length === 0) {
    console.warn('No segments found in manifest')
    return
  }

  // Also parse raw tags manually for complete information
  const lines = rawM3u8Content.value.split('\n')
  const rawSegmentTags: any = {}
  let currentSegmentTags: string[] = []
  let segmentIndex = 0

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim()

    if (line.startsWith('#EXT') || line.startsWith('#OATCLS')) {
      currentSegmentTags.push(line)
    } else if (line && !line.startsWith('#') && !line.includes('.m3u8')) {
      rawSegmentTags[segmentIndex] = [...currentSegmentTags]
      segmentIndex++
      currentSegmentTags = []
    }
  }

  // Combine parsed segments with raw tags
  const segmentsWithTags = parsedManifest.segments.map((segment: any, index: number) => {
    const rawTags = rawSegmentTags[index] || []

    // Parse cue-out/cue-in tags from raw tags
    const cueOut = rawTags.find((t: string) => t.includes('SCTE35-OUT'))
    const cueIn = rawTags.find((t: string) => t.includes('SCTE35-IN') && !t.includes('SCTE35-OUT'))
    const cueOutCont = rawTags.find((t: string) => t.includes('SCTE35-OUT-CONT'))

    // Parse DATERANGE for SCTE35 info
    let dateRangeInfo: any = null
    if (segment.dateRange) {
      const dateRangeTag = rawTags.find((t: string) => t.includes('EXT-X-DATERANGE'))
      dateRangeInfo = {
        id: segment.dateRange.id || null,
        class: segment.dateRange.class || null,
        startDate: segment.dateRange.startDate || null,
        duration: segment.dateRange.duration || null,
        scte35Out: segment.dateRange.scte35Out || null,
        scte35In: segment.dateRange.scte35In || null,
        endOnNext: segment.dateRange.endOnNext || false,
        raw: dateRangeTag || null
      }
    }

    const scte35Tag = rawTags.find((t: string) => t.includes('SCTE35'))

    return {
      uri: segment.uri,
      duration: segment.duration || 0,
      programDateTime: segment.dateTimeString || segment.programDateTime || null,
      tags: rawTags,
      cueOut: cueOut || null,
      cueIn: cueIn || null,
      cueOutCont: cueOutCont || null,
      dateRange: segment.dateRange
        ? rawTags.find((t: string) => t.includes('EXT-X-DATERANGE'))
        : null,
      dateRangeInfo: dateRangeInfo,
      scte35: scte35Tag || null,
      discontinuity: segment.discontinuity || false,
      rawTags: rawTags,
      key: segment.key || null,
      map: segment.map || null,
      byterange: segment.byterange || null
    }
  })

  // Update existing segments with tag information from m3u8
  // This enriches segments that were added by trackCurrentSegment
  segmentsWithTags.forEach((parsedSeg) => {
    const existingIndex = allSegments.value.findIndex((s) => {
      const segmentNum1 = extractSegmentNumber(parsedSeg.uri)
      const segmentNum2 = extractSegmentNumber(s.uri)
      return segmentNum1 && segmentNum2 && segmentNum1 === segmentNum2
    })

    if (existingIndex >= 0) {
      // Enrich existing segment with parsed tags
      allSegments.value[existingIndex] = {
        ...allSegments.value[existingIndex],
        ...parsedSeg,
        uri: allSegments.value[existingIndex].uri // Keep original URI format
      }
    }
  })

  console.log(
    `Parsed m3u8: ${segmentsWithTags.length} segments, Current history: ${allSegments.value.length}`
  )
  console.log('Sample segment:', segmentsWithTags[0])
}

const selectSegment = (segment: any) => {
  selectedSegmentTags.value = segment
}

const updatePlaylistInfo = () => {
  if (!player.value) return

  const tech = player.value.tech({ IWillNotUseThisInPlugins: true })
  if (!tech || !tech.vhs) return

  const vhs = tech.vhs
  const playlist = vhs.playlists?.media()

  if (playlist) {
    playlistInfo.value = {
      version: playlist.version,
      targetDuration: playlist.targetDuration,
      mediaSequence: playlist.mediaSequence,
      discontinuitySequence: playlist.discontinuitySequence,
      playlistType: playlist.playlistType,
      endList: playlist.endList
    }
  }
}

const trackCurrentSegment = () => {
  if (!player.value) {
    return
  }

  const tech = player.value.tech({ IWillNotUseThisInPlugins: true })
  if (!tech || !tech.vhs) {
    return
  }

  const vhs = tech.vhs
  const playlist = vhs.playlists?.media()

  if (!playlist || !playlist.segments || playlist.segments.length === 0) {
    return
  }

  // Update playlist info
  updatePlaylistInfo()

  let currentSegmentUri: string | null = null
  let segment = null
  let segmentIndex = -1

  // Method 1: Get URI from textTracks (most reliable for current segment)
  try {
    const textTracks = tech.textTracks_
    if (textTracks && textTracks[0] && textTracks[0].activeCues && textTracks[0].activeCues[0]) {
      const activeCue = textTracks[0].activeCues[0]
      if (activeCue.value && activeCue.value.uri) {
        currentSegmentUri = activeCue.value.uri as string
        console.log('✓ textTracks URI:', currentSegmentUri)

        // Now find the full segment data from playlist by matching URI
        const foundIndex = playlist.segments.findIndex((seg: any) => {
          const segNum1 = extractSegmentNumber(currentSegmentUri as string)
          const segNum2 = extractSegmentNumber(seg.uri)
          return segNum1 && segNum2 && segNum1 === segNum2
        })

        if (foundIndex >= 0) {
          segmentIndex = foundIndex
          segment = playlist.segments[foundIndex]
          console.log('✓ Found full segment data at index', foundIndex)
        }
      }
    }
  } catch (e) {
    console.log('textTracks error:', e)
  }

  // Method 2: Fallback to VHS mediaIndex
  if (!currentSegmentUri) {
    if (vhs.playlistController_ && typeof vhs.playlistController_.mediaIndex === 'number') {
      segmentIndex = vhs.playlistController_.mediaIndex
    } else if (vhs.playlistController_?.mainSegmentLoader_?.mediaIndex_ !== undefined) {
      segmentIndex = vhs.playlistController_.mainSegmentLoader_.mediaIndex_
    }

    if (segmentIndex >= 0 && segmentIndex < playlist.segments.length) {
      segment = playlist.segments[segmentIndex]
      currentSegmentUri = segment.uri
      console.log('✓ VHS index:', segmentIndex, currentSegmentUri)
    }
  }

  // Method 3: Fallback to time calculation
  if (!currentSegmentUri) {
    const currentTime = player.value.currentTime()
    let accumulatedTime = 0

    for (let i = 0; i < playlist.segments.length; i++) {
      const segmentStart = accumulatedTime
      accumulatedTime += playlist.segments[i].duration

      if (currentTime >= segmentStart && currentTime < accumulatedTime) {
        segmentIndex = i
        segment = playlist.segments[i]
        currentSegmentUri = segment.uri
        console.log('✓ Time calc:', segmentIndex, currentSegmentUri)
        break
      }
    }
  }

  // If we found a segment
  if (currentSegmentUri && segment) {
    const segmentNumber = extractSegmentNumber(currentSegmentUri)
    console.log('Segment number extracted:', segmentNumber)

    // Find matching segment with tags from parsed m3u8
    let matchingSegment = allSegments.value.find((s) => {
      const segmentNum1 = extractSegmentNumber(currentSegmentUri)
      const segmentNum2 = extractSegmentNumber(s.uri)
      const matches = segmentNum1 && segmentNum2 && segmentNum1 === segmentNum2
      return matches
    })

    if (matchingSegment) {
      console.log('✓ Found matching segment with tags')
    }

    // Set current segment with VHS data + parsed m3u8 tags
    currentSegment.value = {
      uri: currentSegmentUri.split('/').slice(-3).join('/'),
      duration: segment.duration || 0,
      programDateTime:
        segment.dateTimeString ||
        segment.programDateTime ||
        matchingSegment?.programDateTime ||
        null,
      discontinuity: segment.discontinuity || matchingSegment?.discontinuity || false,
      key: segment.key || null,
      map: segment.map || null,
      byterange: segment.byterange
        ? `${segment.byterange.length}@${segment.byterange.offset}`
        : null,
      cueOut: segment?.cueOut || null,
      cueIn: segment?.cueIn || null,
      cueOutCont: segment?.cueOutCont || null,
      dateRange: segment?.dateRange || null,
      dateRangeInfo: segment?.dateRangeInfo || null,
      scte35: segment?.scte35 || null,
      rawTags: segment?.rawTags || []
    }

    // Add this segment to history if not already present
    if (!seenSegmentUris.value.has(currentSegmentUri)) {
      console.log('➕ Adding segment #' + segmentNumber + ' to history')
      seenSegmentUris.value.add(currentSegmentUri)

      // Create a copy of the segment data to avoid reference issues
      const segmentCopy = { ...currentSegment.value }

      // Add to the beginning of the array (newest first)
      allSegments.value.unshift(segmentCopy)

      // Keep only last N segments
      if (allSegments.value.length > maxSegmentsToKeep) {
        const removed = allSegments.value.pop()
        if (removed) {
          seenSegmentUris.value.delete(removed.uri)
        }
      }
    } else {
      // Update existing segment with latest info
      const existingIndex = allSegments.value.findIndex((s) => {
        const segmentNum1 = extractSegmentNumber(currentSegmentUri)
        const segmentNum2 = extractSegmentNumber(s.uri)
        return segmentNum1 && segmentNum2 && segmentNum1 === segmentNum2
      })
      if (existingIndex >= 0) {
        // Create a copy to avoid reference issues
        allSegments.value[existingIndex] = { ...currentSegment.value }
      }
    }
  }
}

let m3u8FetchInterval: any = null
let segmentTrackingInterval: any = null

const startM3u8Polling = () => {
  // Fetch immediately
  fetchM3u8Content()

  // Then fetch every 2 seconds to get updated playlist
  m3u8FetchInterval = setInterval(() => {
    fetchM3u8Content()
  }, 2000)
}

onMounted(() => {
  if (props.node) {
    player.value = videojs('videoPlayer', {
      html5: {
        hls: {
          limitRenditionByPlayerDimensions: false
        },
        vhs: {
          // Note: Video.js v7/v8 uses 'vhs' instead of 'hls' in the config object
          limitRenditionByPlayerDimensions: false, // Allows high-res playback even if the player element is small
          smoothQualityChange: true, // Smoother transitions when switching qualities
          overrideNative: true, // Uses Video.js managed playback engine over native Safari/iOS where possible for consistency

          // Live stream specific tuning
          liveSyncDurationCount: 3, // Number of segments from the end to start playback (3 is standard for stability)
          fastQualityChange: true // Drops lower quality buffered frames immediately when a higher quality is selected
        }
      },
      controls: true,
      autoplay: true,
      preload: 'auto'
      // liveui: true
    })

    player.value.on('ready', () => {
      console.log('player is ready')
      ;(window as any).vjsPlayer = player.value
      player.value.qualityMenu()

      // Start polling m3u8 for all segments
      startM3u8Polling()

      // Start tracking current segment immediately and then every second
      console.log('Starting segment tracking interval')
      trackCurrentSegment()
      segmentTrackingInterval = setInterval(() => {
        console.log('Segment tracking tick')
        trackCurrentSegment()
      }, 1000)
    })

    player.value.on('error', function (error: any) {
      console.error('Error occurred. Reloading video...', error)
      reloadVideo()
    })
  }
})

onBeforeUnmount(() => {
  console.log('destroying')

  // Clear m3u8 polling interval
  if (m3u8FetchInterval) {
    clearInterval(m3u8FetchInterval)
  }

  // Clear segment tracking interval
  if (segmentTrackingInterval) {
    clearInterval(segmentTrackingInterval)
  }

  if (player.value) {
    player.value.dispose()
  }
})
</script>

<style scoped>
/* Main Layout */
.livestream-container {
  display: flex;
  gap: 1.5rem;
  min-height: 100vh;
  background: linear-gradient(135deg, #0f0f23 0%, #1a1a2e 100%);
  padding: 1rem;
}

.video-section {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 0;
}

.video-wrapper {
  width: 100%;
  max-width: 100%;
  aspect-ratio: 16/9;
  border-radius: 16px;
  overflow: hidden;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
  position: relative;
}

#videoPlayer {
  width: 100%;
  height: 100%;
}

.video-js {
  width: 100%;
  height: 100%;
}

/* Ad Break Overlay */
.ad-overlay {
  position: absolute;
  top: 20px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 1000;
  pointer-events: none;
}

.ad-label {
  background: linear-gradient(135deg, rgba(244, 67, 54, 0.95) 0%, rgba(211, 47, 47, 0.95) 100%);
  backdrop-filter: blur(10px);
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-radius: 12px;
  padding: 12px 24px;
  display: flex;
  align-items: center;
  gap: 12px;
  box-shadow: 0 8px 32px rgba(244, 67, 54, 0.5);
  animation:
    adPulse 2s infinite,
    slideDown 0.5s ease-out;
}

@keyframes adPulse {
  0%,
  100% {
    box-shadow: 0 8px 32px rgba(244, 67, 54, 0.5);
    transform: scale(1);
  }

  50% {
    box-shadow: 0 8px 40px rgba(244, 67, 54, 0.7);
    transform: scale(1.02);
  }
}

@keyframes slideDown {
  from {
    opacity: 0;
    transform: translateY(-20px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.ad-icon {
  font-size: 24px;
  animation: iconBounce 1.5s infinite;
}

@keyframes iconBounce {
  0%,
  100% {
    transform: translateY(0);
  }

  50% {
    transform: translateY(-3px);
  }
}

.ad-text {
  color: white;
  font-weight: bold;
  font-size: 18px;
  letter-spacing: 1px;
  text-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
}

.ad-duration,
.ad-elapsed {
  background: rgba(255, 255, 255, 0.2);
  padding: 4px 12px;
  border-radius: 20px;
  color: white;
  font-size: 14px;
  font-weight: 600;
  border: 1px solid rgba(255, 255, 255, 0.3);
}

/* Info Sidebar */
.info-sidebar {
  width: 420px;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  overflow-y: auto;
  padding-right: 0.5rem;
}

.info-sidebar::-webkit-scrollbar {
  width: 6px;
}

.info-sidebar::-webkit-scrollbar-track {
  background: rgba(255, 255, 255, 0.05);
  border-radius: 3px;
}

.info-sidebar::-webkit-scrollbar-thumb {
  background: rgba(76, 175, 80, 0.5);
  border-radius: 3px;
}

.info-sidebar::-webkit-scrollbar-thumb:hover {
  background: rgba(76, 175, 80, 0.7);
}

/* Info Panel Base */
.info-panel {
  background: rgba(255, 255, 255, 0.03);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.3);
  transition: all 0.3s ease;
}

.info-panel:hover {
  border-color: rgba(76, 175, 80, 0.3);
  box-shadow: 0 6px 30px rgba(76, 175, 80, 0.15);
}

.panel-header {
  background: rgba(76, 175, 80, 0.1);
  border-bottom: 1px solid rgba(76, 175, 80, 0.3);
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #4caf50;
  font-weight: 600;
  font-size: 14px;
}

.title-icon {
  font-size: 16px;
}

.panel-content {
  padding: 16px;
  color: #e0e0e0;
  font-size: 13px;
}

/* Live/VOD Badges */
.live-badge {
  background: linear-gradient(135deg, #ff1744 0%, #f50057 100%);
  color: white;
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 11px;
  font-weight: bold;
  display: flex;
  align-items: center;
  gap: 4px;
  animation: pulse 2s infinite;
  box-shadow: 0 2px 10px rgba(255, 23, 68, 0.4);
}

.vod-badge {
  background: rgba(96, 125, 139, 0.3);
  border: 1px solid #607d8b;
  color: #90a4ae;
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 11px;
  font-weight: bold;
  display: flex;
  align-items: center;
  gap: 4px;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
    box-shadow: 0 2px 10px rgba(255, 23, 68, 0.4);
  }

  50% {
    opacity: 0.8;
    box-shadow: 0 2px 20px rgba(255, 23, 68, 0.6);
  }
}

/* Info Rows */
.info-row {
  display: flex;
  justify-content: space-between;
  padding: 8px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.info-row:last-child {
  border-bottom: none;
}

.info-row .label {
  color: #9e9e9e;
  font-size: 12px;
}

.info-row .value {
  color: #4caf50;
  font-weight: 500;
  text-align: right;
}

.info-row .value.time {
  color: #64b5f6;
  font-size: 11px;
}

.info-row .label.warning {
  color: #ff9800;
}

/* Segment URI Display */
.segment-uri-display {
  background: rgba(0, 0, 0, 0.3);
  padding: 10px 12px;
  border-radius: 8px;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: #64b5f6;
  word-break: break-all;
  margin-bottom: 12px;
  border-left: 3px solid #4caf50;
}

/* Cue Info */
.cue-info {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: 8px;
  margin: 8px 0;
}

.cue-info.cue-out {
  background: rgba(244, 67, 54, 0.15);
  border-left: 4px solid #f44336;
}

.cue-info.cue-in {
  background: rgba(76, 175, 80, 0.15);
  border-left: 4px solid #4caf50;
}

.cue-info.cue-cont {
  background: rgba(255, 193, 7, 0.15);
  border-left: 4px solid #ffc107;
}

.cue-icon {
  font-size: 20px;
}

.cue-details {
  flex: 1;
}

.cue-label {
  font-size: 11px;
  font-weight: bold;
  text-transform: uppercase;
  opacity: 0.8;
  margin-bottom: 4px;
}

.cue-value {
  font-size: 13px;
  font-weight: 500;
  color: #fff;
}

/* Segments Panel */
.segments-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  max-height: 600px;
}

.segments-panel .panel-content {
  padding: 0;
}

.toggle-btn {
  background: rgba(76, 175, 80, 0.2);
  border: 1px solid rgba(76, 175, 80, 0.5);
  color: #4caf50;
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
}

.toggle-btn:hover {
  background: rgba(76, 175, 80, 0.3);
  border-color: #4caf50;
}

.segments-list-wrapper {
  overflow-y: auto;
  max-height: 500px;
}

.segments-list-wrapper::-webkit-scrollbar {
  width: 8px;
}

.segments-list-wrapper::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.2);
}

.segments-list-wrapper::-webkit-scrollbar-thumb {
  background: rgba(76, 175, 80, 0.4);
  border-radius: 4px;
}

.segments-list-wrapper::-webkit-scrollbar-thumb:hover {
  background: rgba(76, 175, 80, 0.6);
}

/* Segment Card */
.segment-card {
  background: rgba(255, 255, 255, 0.03);
  border-left: 3px solid transparent;
  margin: 8px;
  padding: 12px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.segment-card:hover {
  background: rgba(255, 255, 255, 0.08);
  border-left-color: rgba(76, 175, 80, 0.5);
}

.segment-card.active {
  background: rgba(76, 175, 80, 0.15);
  border-left-color: #4caf50;
  box-shadow: 0 2px 10px rgba(76, 175, 80, 0.2);
}

.segment-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.segment-header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.segment-nav-btn {
  background: rgba(76, 175, 80, 0.15);
  border: 1px solid rgba(76, 175, 80, 0.3);
  border-radius: 4px;
  padding: 4px 8px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  justify-content: center;
}

.segment-nav-btn:hover {
  background: rgba(76, 175, 80, 0.3);
  border-color: rgba(76, 175, 80, 0.5);
  transform: scale(1.05);
}

.segment-nav-btn:active {
  transform: scale(0.98);
}

.segment-number {
  color: #4caf50;
  font-weight: bold;
  font-size: 12px;
}

.segment-duration {
  background: rgba(76, 175, 80, 0.2);
  color: #4caf50;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}

.segment-body {
  margin-bottom: 8px;
}

.segment-uri {
  font-family: 'Courier New', monospace;
  font-size: 11px;
  color: #b0bec5;
  word-break: break-all;
  margin-bottom: 6px;
}

.segment-card.active .segment-uri {
  color: #81c784;
  font-weight: 600;
}

.segment-metadata {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 6px;
}

.segment-time {
  font-size: 11px;
  color: #64b5f6;
  display: flex;
  align-items: center;
  gap: 4px;
}

.segment-detail {
  font-size: 10px;
  color: #9e9e9e;
  display: flex;
  align-items: center;
  gap: 4px;
}

/* Segment Tags */
.segment-tags {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.05);
}

.tag-detailed {
  background: rgba(33, 150, 243, 0.1);
  border: 1px solid rgba(33, 150, 243, 0.3);
  border-radius: 8px;
  padding: 10px;
}

.tag-detailed.daterange {
  border-left: 4px solid #2196f3;
}

.tag-header {
  font-size: 11px;
  font-weight: bold;
  color: #64b5f6;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.tag-body {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.tag-item {
  font-size: 10px;
  color: #b0bec5;
  display: flex;
  gap: 6px;
}

.tag-label {
  color: #64b5f6;
  font-weight: 600;
}

.tag-item.scte35 {
  flex-direction: column;
  gap: 4px;
}

.scte35-preview {
  background: rgba(156, 39, 176, 0.15);
  padding: 6px 8px;
  border-radius: 4px;
  font-family: 'Courier New', monospace;
  color: #ba68c8;
  word-break: break-all;
  font-size: 9px;
}

.tag-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.badge {
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 10px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.badge.cue-out {
  background: rgba(244, 67, 54, 0.2);
  border: 1px solid rgba(244, 67, 54, 0.5);
  color: #ef5350;
}

.badge.cue-in {
  background: rgba(76, 175, 80, 0.2);
  border: 1px solid rgba(76, 175, 80, 0.5);
  color: #66bb6a;
}

.badge.cue-cont {
  background: rgba(255, 193, 7, 0.2);
  border: 1px solid rgba(255, 193, 7, 0.5);
  color: #ffd54f;
}

.badge.discontinuity {
  background: rgba(255, 87, 34, 0.2);
  border: 1px solid rgba(255, 87, 34, 0.5);
  color: #ff7043;
}

.badge.key {
  background: rgba(103, 58, 183, 0.2);
  border: 1px solid rgba(103, 58, 183, 0.5);
  color: #9575cd;
}

.badge.map {
  background: rgba(33, 150, 243, 0.2);
  border: 1px solid rgba(33, 150, 243, 0.5);
  color: #64b5f6;
}

/* Raw Tags Panel */
.raw-tags-panel {
  max-height: 400px;
  display: flex;
  flex-direction: column;
}

.raw-tags-panel .panel-header {
  background: rgba(255, 152, 0, 0.1);
  border-bottom-color: rgba(255, 152, 0, 0.3);
}

.raw-tags-panel .panel-title {
  color: #ffa726;
}

.close-btn {
  background: rgba(244, 67, 54, 0.2);
  border: 1px solid rgba(244, 67, 54, 0.5);
  color: #f44336;
  padding: 4px 12px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
  font-weight: bold;
  transition: all 0.2s ease;
}

.close-btn:hover {
  background: rgba(244, 67, 54, 0.3);
  border-color: #f44336;
}

.raw-tags-panel .panel-content {
  overflow-y: auto;
  max-height: 320px;
}

.raw-tags-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.raw-tag {
  background: rgba(255, 152, 0, 0.1);
  border-left: 3px solid #ff9800;
  padding: 8px 12px;
  border-radius: 4px;
  font-family: 'Courier New', monospace;
  font-size: 11px;
  color: #ffb74d;
  word-break: break-all;
}

.no-data {
  color: #757575;
  font-style: italic;
  text-align: center;
  padding: 32px 16px;
}

/* Mobile Toggle Button */
.mobile-toggle {
  display: none;
  width: 100%;
  background: rgba(76, 175, 80, 0.2);
  border: 1px solid rgba(76, 175, 80, 0.5);
  color: #4caf50;
  padding: 12px;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
  margin-bottom: 1rem;
}

.mobile-toggle:hover {
  background: rgba(76, 175, 80, 0.3);
  border-color: #4caf50;
}

/* Mobile Responsive Styles */
@media (max-width: 1024px) {
  .livestream-container {
    flex-direction: column;
    min-height: 100vh;
    height: auto;
    padding: 0.5rem;
    overflow-y: auto;
  }

  .video-section {
    width: 100%;
    max-width: 100%;
  }

  .video-wrapper {
    border-radius: 12px;
  }

  .info-sidebar {
    width: 100%;
    max-height: none;
    margin-top: 1rem;
  }

  .mobile-toggle {
    display: block;
  }

  .info-sidebar.collapsed .info-panel {
    display: none;
  }

  .info-sidebar.collapsed .mobile-toggle {
    margin-bottom: 0;
  }
}

@media (max-width: 768px) {
  .livestream-container {
    padding: 0.5rem;
    gap: 0.75rem;
  }

  .video-section {
    min-height: 200px;
  }

  .video-wrapper {
    border-radius: 8px;
  }

  /* Ad Overlay - smaller on mobile */
  .ad-overlay {
    top: 10px;
  }

  .ad-label {
    padding: 8px 16px;
    gap: 8px;
    border-radius: 8px;
  }

  .ad-icon {
    font-size: 18px;
  }

  .ad-text {
    font-size: 14px;
  }

  .ad-duration,
  .ad-elapsed {
    font-size: 11px;
    padding: 3px 8px;
  }

  /* Info Panels */
  .info-panel {
    border-radius: 8px;
  }

  .panel-header {
    padding: 10px 12px;
  }

  .panel-title {
    font-size: 13px;
  }

  .title-icon {
    font-size: 14px;
  }

  .panel-content {
    padding: 12px;
    font-size: 12px;
  }

  /* Segment Cards */
  .segment-card {
    margin: 6px;
    padding: 10px;
  }

  .segment-header {
    margin-bottom: 6px;
  }

  .segment-number,
  .segment-duration {
    font-size: 11px;
  }

  .segment-uri {
    font-size: 10px;
  }

  .segment-time,
  .segment-detail {
    font-size: 10px;
  }

  /* Tags */
  .tag-detailed {
    padding: 8px;
  }

  .tag-header {
    font-size: 10px;
  }

  .tag-item {
    font-size: 9px;
  }

  .badge {
    padding: 3px 8px;
    font-size: 9px;
  }

  /* Cue Info */
  .cue-info {
    padding: 10px;
    gap: 10px;
  }

  .cue-icon {
    font-size: 16px;
  }

  .cue-label {
    font-size: 10px;
  }

  .cue-value {
    font-size: 12px;
  }

  /* Segments List */
  .segments-list-wrapper {
    max-height: 400px;
  }

  .segments-panel {
    max-height: 500px;
  }

  /* Info Rows */
  .info-row {
    padding: 6px 0;
  }

  .info-row .label {
    font-size: 11px;
  }

  .info-row .value {
    font-size: 11px;
  }

  .segment-uri-display {
    font-size: 11px;
    padding: 8px 10px;
  }

  /* Badges */
  .live-badge,
  .vod-badge {
    font-size: 10px;
    padding: 3px 10px;
  }
}

@media (max-width: 480px) {
  .livestream-container {
    padding: 0.5rem;
    gap: 0.5rem;
  }

  .video-section {
    padding: 0;
    min-height: 180px;
  }

  .video-wrapper {
    border-radius: 6px;
  }

  .info-sidebar {
    padding-right: 0.25rem;
    margin-top: 0.5rem;
  }

  /* Even smaller text on very small screens */
  .panel-content {
    font-size: 11px;
    padding: 10px;
  }

  .segment-card {
    margin: 4px;
    padding: 8px;
  }

  .segment-uri {
    font-size: 9px;
  }

  .tag-item,
  .badge {
    font-size: 8px;
  }

  .ad-label {
    padding: 6px 12px;
    gap: 6px;
  }

  .ad-text {
    font-size: 12px;
  }

  .ad-icon {
    font-size: 16px;
  }
}
</style>
