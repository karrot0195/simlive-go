<template>
  <div class="player-wrapper">
    <video-player ref="player" class="video-player">
      <media-container>
        <hlsjs-video
          class="video-media"
          :src="videoUrl"
          autoplay
          crossorigin="anonymous"
          muted
          playsinline
          loop
        ></hlsjs-video>

        <media-controls class="controls-host">
          <media-controls-backdrop class="controls-backdrop"></media-controls-backdrop>

          <media-controls-content class="controls-content">
            <media-controls-group class="controls-top" aria-label="Top controls">
              <div class="controls-top-left">
                <media-title class="controls-title"></media-title>
              </div>

              <div class="controls-top-right">
                <div class="menu-bar">
                  <button
                    type="button"
                    commandfor="settings-menu"
                    class="settings-trigger"
                    aria-label="Settings"
                  >
                    <span aria-hidden="true">⚙</span>
                  </button>

                  <media-menu
                    id="settings-menu"
                    side="top"
                    align="end"
                    class="menu"
                  >
                    <media-menu-content class="menu-content">
                      <media-menu-item
                        commandfor="quality-menu"
                        class="menu-item"
                      >
                        <span>Quality</span>
                        <span class="menu-value">
                          <span data-part="hint"></span>
                          <span aria-hidden="true">›</span>
                        </span>
                      </media-menu-item>

                      <media-menu-item
                        commandfor="speed-menu"
                        class="menu-item"
                      >
                        <span>Speed</span>
                        <span class="menu-value">
                          <span data-part="hint"></span>
                          <span aria-hidden="true">›</span>
                        </span>
                      </media-menu-item>

                      <media-menu-item
                        commandfor="audio-menu"
                        class="menu-item"
                      >
                        <span>Audio</span>
                        <span class="menu-value">
                          <span data-part="hint"></span>
                          <span aria-hidden="true">›</span>
                        </span>
                      </media-menu-item>

                      <media-menu-item
                        commandfor="captions-menu"
                        class="menu-item"
                      >
                        <span>Captions</span>
                        <span class="menu-value">
                          <span data-part="hint"></span>
                          <span aria-hidden="true">›</span>
                        </span>
                      </media-menu-item>

                      <media-menu-separator class="menu-separator"></media-menu-separator>

                      <media-menu-item
                        class="menu-item"
                        @click="copyLink"
                      >
                        <span>Copy link</span>
                      </media-menu-item>
                    </media-menu-content>

                    <!-- Quality submenu -->
                    <media-menu-content id="quality-menu" class="menu-panel">
                      <media-menu-item class="menu-back">
                        <span aria-hidden="true">‹</span>
                        Quality
                      </media-menu-item>
                      <media-quality-radio-group
                        class="menu-group"
                        label="Quality"
                      >
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

                    <!-- Playback rate submenu -->
                    <media-menu-content id="speed-menu" class="menu-panel">
                      <media-menu-item class="menu-back">
                        <span aria-hidden="true">‹</span>
                        Speed
                      </media-menu-item>
                      <media-playback-rate-radio-group class="menu-group">
                        <template>
                          <media-menu-radio-item class="menu-item">
                            <span data-part="label"></span>
                            <span data-part="hint" class="menu-badge"></span>
                            <media-menu-item-indicator
                              force-mount
                              class="menu-indicator"
                            >✓</media-menu-item-indicator>
                          </media-menu-radio-item>
                        </template>
                      </media-playback-rate-radio-group>
                    </media-menu-content>

                    <!-- Audio track submenu -->
                    <media-menu-content id="audio-menu" class="menu-panel">
                      <media-menu-item class="menu-back">
                        <span aria-hidden="true">‹</span>
                        Audio
                      </media-menu-item>
                      <media-audio-track-radio-group class="menu-group">
                        <template>
                          <media-menu-radio-item class="menu-item">
                            <span data-part="label"></span>
                            <media-menu-item-indicator
                              force-mount
                              class="menu-indicator"
                            >✓</media-menu-item-indicator>
                          </media-menu-radio-item>
                        </template>
                      </media-audio-track-radio-group>
                    </media-menu-content>

                    <!-- Captions submenu -->
                    <media-menu-content id="captions-menu" class="menu-panel">
                      <media-menu-item class="menu-back">
                        <span aria-hidden="true">‹</span>
                        Captions
                      </media-menu-item>
                      <media-captions-radio-group class="menu-group">
                        <template>
                          <media-menu-radio-item class="menu-item">
                            <span data-part="label"></span>
                            <media-menu-item-indicator
                              force-mount
                              class="menu-indicator"
                            >✓</media-menu-item-indicator>
                          </media-menu-radio-item>
                        </template>
                      </media-captions-radio-group>
                    </media-menu-content>
                  </media-menu>
                </div>
              </div>
            </media-controls-group>

            <media-controls-group class="controls-bottom" aria-label="Playback controls">
              <div class="controls-row">
                <media-play-button class="ctrl-button ctrl-play">
                  <span class="paused">▶</span>
                  <span class="playing">⏸</span>
                </media-play-button>

                <media-mute-button class="ctrl-button ctrl-mute">
                  <span class="muted">🔇</span>
                  <span class="unmuted">🔊</span>
                </media-mute-button>

                <media-volume-slider class="ctrl-volume-slider" aria-label="Volume"></media-volume-slider>

                <div class="ctrl-time">
                  <media-time type="current" class="time-current"></media-time>
                  <span class="time-divider">/</span>
                  <media-time type="duration" class="time-duration"></media-time>
                </div>

                <div class="ctrl-spacer"></div>

                <media-fullscreen-button class="ctrl-button ctrl-fullscreen">
                  <span class="enter">⛶</span>
                  <span class="exit">⤢</span>
                </media-fullscreen-button>
              </div>

              <media-time-slider class="ctrl-time-slider" aria-label="Seek"></media-time-slider>
            </media-controls-group>
          </media-controls-content>
        </media-controls>
      </media-container>
    </video-player>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

// Video.js HTML web component definitions
import '@videojs/html/video/player';
import '@videojs/html/ui/container';
import '@videojs/html/media/hlsjs-video';

import '@videojs/html/ui/controls';
import '@videojs/html/ui/play-button';
import '@videojs/html/ui/mute-button';
import '@videojs/html/ui/volume-slider';
import '@videojs/html/ui/fullscreen-button';
import '@videojs/html/ui/time';
import '@videojs/html/ui/time-slider';
import '@videojs/html/ui/title';

import '@videojs/html/ui/menu';
import '@videojs/html/ui/quality-radio-group';
import '@videojs/html/ui/playback-rate-radio-group';
import '@videojs/html/ui/audio-track-radio-group';
import '@videojs/html/ui/captions-radio-group';

// Query parameter or default channel id
const params = new URLSearchParams(window.location.search);
const channelId: string =
  params.get('channel_id') ?? '7261da4b-9c45-4619-bfb0-a6119dc7b6e6';

// API base switches by mode:
//   dev  -> http://localhost:8080/live  (.env.development)
//   prod -> /live                        (.env.production)
const apiBase: string = import.meta.env.VITE_API_BASE
const videoUrl = computed<string>(() => `${apiBase}/${channelId}/playlist.m3u8`)

async function copyLink(): Promise<void> {
  try {
    await navigator.clipboard.writeText(window.location.href)
  } catch {
    // ignore clipboard failures (permissions, insecure context, etc.)
  }
}
</script>

<style scoped>
/* ---- Player wrapper: real 16:9 box that holds the entire UI ---- */
.player-wrapper {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  background: #000;
  border-radius: 0.75rem;
  overflow: hidden;
  color: #fff;
}

.video-player,
.video-media,
media-container {
  display: block;
  width: 100%;
  height: 100%;
}

/* The media element itself needs to fill its host */
.video-media {
  object-fit: contain;
}

/* ---- Controls overlay ---- */
.controls-host {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  pointer-events: none;
}

.controls-host > * {
  pointer-events: auto;
}

.controls-backdrop {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.7), rgba(0, 0, 0, 0) 50%);
  pointer-events: none;
}

.controls-content {
  position: relative;
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 0.75rem;
  gap: 0.5rem;
}

.controls-top,
.controls-bottom {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.controls-top {
  justify-content: space-between;
}

.controls-top-right {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.controls-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
}

.ctrl-spacer {
  flex: 1;
}

.ctrl-time-slider {
  width: 100%;
}

.ctrl-time {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-variant-numeric: tabular-nums;
  font-size: 0.875rem;
}

.time-divider {
  opacity: 0.5;
}

.ctrl-button {
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  padding: 0.25rem 0.5rem;
  font-size: 1.1rem;
  line-height: 1;
  border-radius: 0.375rem;
}

.ctrl-button:hover {
  background: rgba(255, 255, 255, 0.1);
}

/* ---- Settings menu (anchored to the gear button) ---- */
.menu-bar {
  position: relative;
}

.settings-trigger {
  background: rgba(0, 0, 0, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #fff;
  padding: 0.4rem 0.7rem;
  border-radius: 0.5rem;
  cursor: pointer;
  font-size: 1.1rem;
  line-height: 1;
  backdrop-filter: blur(6px);
}

.settings-trigger:hover {
  background: rgba(0, 0, 0, 0.8);
}

.menu {
  min-width: 220px;
  background: rgba(15, 15, 15, 0.95);
  color: #fff;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 0.5rem;
  padding: 0.25rem;
  backdrop-filter: blur(12px);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}

.menu-content {
  display: flex;
  flex-direction: column;
}

.menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  min-height: 2rem;
  padding: 0.4rem 0.6rem;
  border-radius: 0.375rem;
  cursor: pointer;
  background: transparent;
  border: 0;
  color: inherit;
  font: inherit;
  text-align: left;
  width: 100%;
}

.menu-item:hover {
  background: rgba(255, 255, 255, 0.08);
}

.menu-value {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  opacity: 0.7;
  font-size: 0.85em;
}

.menu-separator {
  height: 1px;
  background: rgba(255, 255, 255, 0.1);
  margin: 0.25rem 0;
}

.menu-back {
  font-weight: 600;
  cursor: pointer;
  padding: 0.4rem 0.6rem;
}

.menu-panel {
  display: flex;
  flex-direction: column;
}

.menu-group {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
}

.menu-tier {
  margin-left: 0.2rem;
  font-size: 0.7em;
  opacity: 0.7;
}

.menu-badge {
  margin-left: auto;
  opacity: 0.7;
  font-size: 0.8em;
}

.menu-indicator {
  margin-left: 0.5rem;
  opacity: 0;
}

media-menu-radio-item[aria-checked='true'] .menu-indicator {
  opacity: 1;
}
</style>