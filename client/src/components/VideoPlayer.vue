<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { createPlayer } from "@videojs/html";
import { liveVideoFeatures, streamTypeFeature, qualityFeature } from "@videojs/core/dom";
import "@videojs/html/media/hlsjs-video";
import "@videojs/html/live-video/ui";
import '@videojs/html/ui/menu';
import '@videojs/html/ui/quality-radio-group';
import "@videojs/html/ui/mute-button";
import ChannelTabs from './ChannelTabs.vue';
import HlsInspector from './HlsInspector.vue';
import { activeHlsUrl, channelStore } from './channelStore.ts';

// Extend the live-video preset with stream-type + quality so the controller
// exposes the current stream type (live vs. vod) and the quality menu has
// data to render. `liveVideoFeatures` drops quality for plain live streams.
const { PlayerElement } = createPlayer({
  features: [...liveVideoFeatures, streamTypeFeature, qualityFeature],
});

class LiveStreamPlayerElement extends PlayerElement {
  static readonly tagName = "live-stream-player";
}

if (!customElements.get(LiveStreamPlayerElement.tagName)) {
  customElements.define(LiveStreamPlayerElement.tagName, LiveStreamPlayerElement);
}

// --- Active channel / HLS URL ------------------------------------------
//
// `channelStore.activeChannelId` is the single source of truth for what's
// playing. `activeHlsUrl()` returns the master playlist URL for the active
// channel, or null when nothing is selected (the player renders a
// placeholder in that case).

const hlsUrl = computed<string | null>(() => activeHlsUrl());
const hasActiveChannel = computed<boolean>(() => hlsUrl.value !== null);
const activeChannelName = computed<string>(
  () => channelStore.activeChannel?.name ?? "",
);

// --- Resizable sidebar ---
//
// The left panel can be dragged between MIN_WIDTH and MAX_WIDTH. The current
// width is persisted in localStorage so the user's preferred size survives
// reloads. There's also a collapse toggle that shrinks the panel to a thin
// rail.

const STORAGE_KEY = "simlive.sidebarWidth";
const MIN_WIDTH = 240;
const MAX_WIDTH_FRAC = 0.6; // never more than 60% of the viewport
const RAIL_WIDTH = 48;
const DEFAULT_WIDTH = 320;

const isCollapsed = ref(false);
const sidebarWidth = ref(DEFAULT_WIDTH);

function loadStoredWidth(): number {
  if (typeof window === "undefined") return DEFAULT_WIDTH;
  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) return DEFAULT_WIDTH;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n >= MIN_WIDTH ? n : DEFAULT_WIDTH;
}

onMounted(() => {
  sidebarWidth.value = loadStoredWidth();
});

watch(sidebarWidth, (w) => {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, String(Math.round(w)));
});

function clampWidth(w: number, viewportWidth: number): number {
  const max = Math.max(MIN_WIDTH, Math.floor(viewportWidth * MAX_WIDTH_FRAC));
  return Math.min(Math.max(w, MIN_WIDTH), max);
}

const isDragging = ref(false);
let dragStartX = 0;
let dragStartWidth = 0;
let dragViewportWidth = 0;

function onSplitterPointerDown(event: PointerEvent): void {
  if (isCollapsed.value) return;
  isDragging.value = true;
  dragStartX = event.clientX;
  dragStartWidth = sidebarWidth.value;
  dragViewportWidth = window.innerWidth;
  (event.target as HTMLElement).setPointerCapture(event.pointerId);
}

function onSplitterPointerMove(event: PointerEvent): void {
  if (!isDragging.value) return;
  const delta = event.clientX - dragStartX;
  sidebarWidth.value = clampWidth(dragStartWidth + delta, dragViewportWidth);
}

function onSplitterPointerUp(event: PointerEvent): void {
  if (!isDragging.value) return;
  isDragging.value = false;
  try {
    (event.target as HTMLElement).releasePointerCapture(event.pointerId);
  } catch {
    // pointer was already released
  }
}

function toggleCollapsed(): void {
  isCollapsed.value = !isCollapsed.value;
}

const sidebarStyle = computed(() => ({
  width: isCollapsed.value ? `${RAIL_WIDTH}px` : `${sidebarWidth.value}px`,
  flexShrink: 0,
}));

const splitterStyle = computed(() => ({
  width: isCollapsed.value ? "0px" : "4px",
  cursor: isCollapsed.value ? "default" : "col-resize",
}));
</script>

<template>
  <div class="w-screen h-screen overflow-hidden flex flex-row bg-black text-white select-none">
    <live-stream-player class="contents">
      <!-- Session 1: tabbed sidebar (Channels + Session Info) — resizable + collapsible -->
      <aside
        class="relative h-full bg-slate-950 border-r border-slate-800 flex flex-col transition-[width] duration-150"
        :class="{ 'duration-0': isDragging }"
        :style="sidebarStyle"
      >
        <!-- Header bar with collapse button -->
        <div
          class="flex items-center justify-between px-3 py-2 border-b border-slate-800 bg-slate-900/80"
        >
          <span
            v-if="!isCollapsed"
            class="text-[10px] uppercase tracking-widest text-slate-400 font-semibold"
          >
            Session 1
          </span>
          <button
            type="button"
            class="ml-auto p-1.5 rounded-md hover:bg-slate-800 text-slate-300 hover:text-white transition"
            :aria-label="isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'"
            :aria-expanded="!isCollapsed"
            @click="toggleCollapsed"
          >
            <svg
              v-if="isCollapsed"
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 20 20"
              fill="currentColor"
              class="w-4 h-4"
            >
              <path
                fill-rule="evenodd"
                d="M7.21 14.77a.75.75 0 0 1 .02-1.06L10.94 10 7.23 6.29a.75.75 0 1 1 1.04-1.08l4.25 4.25a.75.75 0 0 1 0 1.08l-4.25 4.25a.75.75 0 0 1-1.06-.02Z"
                clip-rule="evenodd"
              />
            </svg>
            <svg
              v-else
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 20 20"
              fill="currentColor"
              class="w-4 h-4"
            >
              <path
                fill-rule="evenodd"
                d="M12.79 5.23a.75.75 0 0 1 0 1.06L9.06 10l3.73 3.71a.75.75 0 1 1-1.06 1.06l-4.25-4.25a.75.75 0 0 1 0-1.06l4.25-4.24a.75.75 0 0 1 1.06 0Z"
                clip-rule="evenodd"
              />
            </svg>
          </button>
        </div>

        <!-- Collapsed rail -->
        <div
          v-if="isCollapsed"
          class="flex-1 flex flex-col items-center pt-4 gap-4"
        >
          <div class="text-[10px] tracking-widest text-slate-500 [writing-mode:vertical-rl] rotate-180 uppercase">
            Sidebar
          </div>
        </div>

        <!-- Expanded panel -->
        <div v-else class="flex-1 min-h-0">
          <ChannelTabs />
        </div>

        <!-- Drag handle -->
        <div
          v-if="!isCollapsed"
          class="absolute top-0 right-0 h-full bg-slate-700/40 hover:bg-sky-500/60 transition-colors"
          :class="{ 'bg-sky-500/80': isDragging }"
          :style="splitterStyle"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize info panel"
          @pointerdown="onSplitterPointerDown"
          @pointermove="onSplitterPointerMove"
          @pointerup="onSplitterPointerUp"
          @pointercancel="onSplitterPointerUp"
        ></div>
      </aside>

      <!-- Session 2: live HLS video with mute + live + quality menu -->
      <main class="flex-1 min-w-0 h-full">
        <media-container class="relative block w-full h-full">
          <!-- Active channel: real player + HLS inspector -->
          <template v-if="hasActiveChannel">
            <hlsjs-video
              :key="channelStore.changeToken"
              :src="hlsUrl ?? ''"
              autoplay
              muted
              playsinline
              class="block w-full h-full object-contain bg-black"
            ></hlsjs-video>

            <!-- Hidden inspector: bridges the hls.js engine into the shared store -->
            <HlsInspector />
          </template>

          <!-- No channel selected: empty-state placeholder -->
          <div
            v-else
            class="absolute inset-0 flex flex-col items-center justify-center text-center px-6 bg-gradient-to-b from-black via-slate-950 to-black"
            role="status"
            aria-live="polite"
          >
            <div
              class="w-16 h-16 rounded-full bg-slate-900/80 border border-slate-800 flex items-center justify-center mb-4"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                class="w-8 h-8 text-slate-500"
                aria-hidden="true"
              >
                <rect x="2" y="6" width="14" height="12" rx="2" />
                <path d="m22 8-6 4 6 4V8Z" />
              </svg>
            </div>
            <h2 class="text-lg font-semibold text-slate-200">
              No channel selected
            </h2>
            <p class="mt-1 text-sm text-slate-500 max-w-sm">
              Open the <strong class="text-slate-300">Channels</strong> tab on
              the left and pick a playout channel to start watching.
            </p>
          </div>

          <!-- Top-left: currently-playing label (only when playing) -->
          <div
            v-if="hasActiveChannel"
            class="absolute top-3 left-3 z-20 pointer-events-none"
          >
            <div
              class="px-3 py-1.5 text-xs font-semibold text-slate-100 bg-slate-900/70 border border-white/20 rounded-full backdrop-blur-md shadow-lg shadow-black/30"
            >
              {{ activeChannelName }}
            </div>
          </div>

          <!-- Top-left: mute / volume (only when playing) -->
          <div v-if="hasActiveChannel" class="absolute top-3 left-3 z-10">
            <media-mute-button
              class="px-3 py-2 inline-flex items-center gap-2 text-sm font-medium text-white bg-slate-900/70 border border-white/20 rounded-full backdrop-blur-md hover:bg-slate-800/90 transition shadow-lg shadow-black/30 cursor-pointer"
            >
              <!-- Muted icon (speaker with X) -->
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                class="icon-muted w-4 h-4"
                aria-hidden="true"
              >
                <path d="M11 5L6 9H2v6h4l5 4V5z" />
                <line x1="22" y1="9" x2="16" y2="15" />
                <line x1="16" y1="9" x2="22" y2="15" />
              </svg>
              <!-- Unmuted icon (speaker waves) -->
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                class="icon-unmuted w-4 h-4"
                aria-hidden="true"
              >
                <path d="M11 5L6 9H2v6h4l5 4V5z" />
                <path d="M19.07 4.93a10 10 0 0 1 0 14.14" />
                <path d="M15.54 8.46a5 5 0 0 1 0 7.07" />
              </svg>
              <span class="label-muted">Unmute</span>
              <span class="label-unmuted">Mute</span>
            </media-mute-button>
          </div>

          <!-- Top-right: quality menu (only when playing) -->
          <div v-if="hasActiveChannel" class="absolute top-3 right-3 z-10 flex gap-2">
            <button
              type="button"
              commandfor="quality-menu"
              class="settings-trigger px-4 py-2 text-sm font-medium text-white bg-slate-900/70 border border-white/20 rounded-full backdrop-blur-md hover:bg-slate-800/90 transition shadow-lg shadow-black/30"
            >
              Quality
            </button>
            <media-menu
              id="quality-menu"
              side="bottom"
              align="end"
              class="menu"
            >
              <media-menu-content>
                <media-quality-radio-group label="Quality">
                  <template>
                    <media-menu-radio-item class="menu-item">
                      <span>
                        <span data-part="label"></span>
                        <sup data-part="tier" class="menu-tier"></sup>
                      </span>
                      <span data-part="badge" class="menu-badge"></span>
                      <media-menu-item-indicator
                        force-mount
                        class="menu-indicator"
                      >✓</media-menu-item-indicator>
                    </media-menu-radio-item>
                  </template>
                </media-quality-radio-group>
              </media-menu-content>
            </media-menu>
          </div>

          <!-- Bottom-left: live badge (only when playing) -->
          <div v-if="hasActiveChannel" class="absolute bottom-3 left-3 z-10">
            <media-live-button></media-live-button>
          </div>
        </media-container>
      </main>
    </live-stream-player>
  </div>
</template>

<style scoped>
/* Hide the quality button entirely when the stream exposes only one
   rendition (or none yet). Mirrors the pattern from the official
   "Add a quality selector" guide. */
:has(media-quality-radio-group[data-availability="unavailable"]) .settings-trigger {
  display: none;
}

/* Mute button icon + label switching. Follows the data-attribute pattern
   from the official "media-mute-button" docs. */
media-mute-button .icon-muted,
media-mute-button .icon-unmuted,
media-mute-button .label-muted,
media-mute-button .label-unmuted {
  display: none;
}
media-mute-button[data-muted] .icon-muted {
  display: inline-block;
}
media-mute-button[data-muted] .label-muted {
  display: inline;
}
media-mute-button:not([data-muted]) .icon-unmuted {
  display: inline-block;
}
media-mute-button:not([data-muted]) .label-unmuted {
  display: inline;
}

.menu {
  --media-menu-side-offset: 8px;
  box-sizing: border-box;
  min-width: 180px;
  max-width: var(--media-menu-available-width, var(--media-popover-available-width, none));
  max-height: var(--media-menu-available-height, var(--media-popover-available-height, none));
  padding: 6px;
  overflow: auto;
  overscroll-behavior: none;
  font-size: 14px;
  color: white;
  background: rgba(15, 23, 42, 0.95);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  backdrop-filter: blur(10px);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
}

:deep(media-quality-radio-group) {
  display: grid;
  gap: 2px;
}

:deep(.menu-item) {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  min-height: 32px;
  padding: 0 10px;
  font: inherit;
  color: inherit;
  cursor: pointer;
  background: none;
  border: 0;
  border-radius: 6px;
}

:deep(.menu-item[data-highlighted]) {
  background: rgba(255, 255, 255, 0.16);
}

:deep(.menu-tier) {
  margin-left: 2px;
  font-size: 10px;
}

:deep(.menu-badge) {
  margin-left: auto;
  color: rgba(255, 255, 255, 0.72);
}

:deep(.menu-indicator) {
  opacity: 0;
}

:deep(media-menu-radio-item[aria-checked="true"]) .menu-indicator {
  opacity: 1;
}
</style>
