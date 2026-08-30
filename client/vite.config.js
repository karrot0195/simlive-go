import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// Tags used in the VideoPlayer template. List them explicitly per the
// Video.js + Vue integration guide; avoid broad prefix rules.
const videoJsElements = new Set([
  'video-player',
  'media-container',
  'hlsjs-video',
  'media-controls',
  'media-controls-backdrop',
  'media-controls-content',
  'media-controls-group',
  'media-title',
  'media-play-button',
  'media-mute-button',
  'media-volume-slider',
  'media-time',
  'media-time-slider',
  'media-fullscreen-button',
  'media-menu',
  'media-menu-content',
  'media-menu-item',
  'media-menu-separator',
  'media-menu-radio-item',
  'media-menu-item-indicator',
  'media-quality-radio-group',
  'media-playback-rate-radio-group',
  'media-audio-track-radio-group',
  'media-captions-radio-group',
])

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Load all env files explicitly so we can read VITE_API_BASE in the proxy target
  const env = loadEnv(mode, process.cwd(), '')
  const apiBase = env.VITE_API_BASE || 'http://localhost:8080/live'

  return {
    plugins: [
      vue({
        template: {
          compilerOptions: {
            // Per the official Video.js + Vue guide, list the exact Video.js
            // HTML tags used in templates. Listing them explicitly avoids
            // broad prefix matches that can hide misspelled Vue components
            // and keeps Vue from treating these custom elements as Vue
            // components (which causes the "Active effect was not restored"
            // / stack overflow errors).
            isCustomElement: (tag) => videoJsElements.has(tag)
          }
        }
      }),
      tailwindcss()
    ],
    server: {
      port: 5173,
      open: true,
      proxy: {
        // Proxy /live/* -> backend during development
        '/live': {
          target: apiBase,
          changeOrigin: true,
          secure: false
        }
      }
    }
  }
})