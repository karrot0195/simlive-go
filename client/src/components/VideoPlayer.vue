<script setup lang="ts">
import { createPlayer } from "@videojs/html";
import { liveVideoFeatures, streamTypeFeature, qualityFeature } from "@videojs/core/dom";
import "@videojs/html/media/hlsjs-video";
import "@videojs/html/live-video/ui";
import '@videojs/html/ui/menu';
import '@videojs/html/ui/quality-radio-group';
import SessionInfoPanel from './SessionInfoPanel.vue';

// Extend the live-video preset with the stream-type feature so the controller
// exposes the current stream type (live vs. vod), and add the quality feature
// so the quality menu can offer manual rendition selection (liveVideoFeatures
// drops quality for plain live streams).
const { PlayerElement } = createPlayer({
  features: [...liveVideoFeatures, streamTypeFeature, qualityFeature],
});

class LiveStreamPlayerElement extends PlayerElement {
  static readonly tagName = "live-stream-player";
}

if (!customElements.get(LiveStreamPlayerElement.tagName)) {
  customElements.define(LiveStreamPlayerElement.tagName, LiveStreamPlayerElement);
}

const hlsUrl =
  "http://localhost:8080/live/7261da4b-9c45-4619-bfb0-a6119dc7b6e6/playlist.m3u8"
</script>

<template>
  <div class="w-screen h-screen overflow-hidden flex flex-row bg-black text-white">
    <live-stream-player class="contents">
      <!-- Session 1: stream info panel -->
      <div class="w-2/5 h-full overflow-hidden border-r border-gray-800">
        <SessionInfoPanel />
      </div>

      <!-- Session 2: live HLS video with live button + quality menu -->
      <div class="w-3/5 h-full">
        <media-container class="relative block w-full h-full">
          <hlsjs-video
            :src="hlsUrl"
            autoplay
            muted
            playsinline
            class="block w-full h-full object-contain bg-black"
          ></hlsjs-video>

          <div class="absolute top-3 right-3 z-10 flex gap-2">
            <button
              type="button"
              commandfor="quality-menu"
              class="settings-trigger px-4 py-2 text-sm font-medium text-black bg-white/75 border border-white/35 rounded-full backdrop-blur-md hover:bg-white/90 transition"
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

          <div class="absolute bottom-3 left-3 z-10">
            <media-live-button></media-live-button>
          </div>
        </media-container>
      </div>
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
  background: rgba(0, 0, 0, 0.88);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  backdrop-filter: blur(10px);
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
