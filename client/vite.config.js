import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// Tags used in the VideoPlayer template. List them explicitly per the
// Video.js + Vue integration guide; avoid broad prefix rules.
const videoJsElements = new Set([
  'live-stream-player',
  'media-container',
  'hlsjs-video',
  'media-live-button',
  'media-mute-button',
  'media-menu',
  'media-menu-content',
  'media-quality-radio-group',
  'media-menu-radio-item',
  'media-menu-item-indicator',
  'session-info-panel',
  'hls-inspector',
  'channel-tabs',
  'channel-list-panel',
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
        },
        // Proxy /api/* -> backend during development (REST surface for
        // channel CRUD + start/stop + ESPX list).
        '/api': {
          target: apiBase,
          changeOrigin: true,
          secure: false
        }
      }
    }
  }
})