// Type declarations for the v10 web components from @videojs/html.
// These let TypeScript validate templates and attribute bindings.

import 'vue'

declare module 'vue' {
  interface GlobalComponents {
    'live-stream-player': import('vue').DefineComponent<{}>
    'media-container': import('vue').DefineComponent<{}>
    'hlsjs-video': import('vue').DefineComponent<{
      src?: string
      autoplay?: boolean
      muted?: boolean
      playsinline?: boolean
      crossorigin?: string
      loop?: boolean
      poster?: string
    }>
    'media-live-button': import('vue').DefineComponent<{}>
    'media-mute-button': import('vue').DefineComponent<{}>
    'media-menu': import('vue').DefineComponent<{}>
    'media-menu-content': import('vue').DefineComponent<{}>
    'media-quality-radio-group': import('vue').DefineComponent<{}>
    'media-menu-radio-item': import('vue').DefineComponent<{}>
    'media-menu-item-indicator': import('vue').DefineComponent<{}>
    'session-info-panel': import('vue').DefineComponent<{}>
    'hls-inspector': import('vue').DefineComponent<{}>
    'channel-tabs': import('vue').DefineComponent<{}>
    'channel-list-panel': import('vue').DefineComponent<{}>
  }
}

export {}
