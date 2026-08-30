// Type declarations for the v10 web components from @videojs/html.
// These let TypeScript validate templates and attribute bindings.

import 'vue'

declare module 'vue' {
  interface GlobalComponents {
    'video-player': import('vue').DefineComponent<{}>
    'video-skin': import('vue').DefineComponent<{}>
    'hlsjs-video': import('vue').DefineComponent<{
      src?: string
      autoplay?: boolean
      muted?: boolean
      playsinline?: boolean
      crossorigin?: string
      loop?: boolean
      poster?: string
    }>
  }
}

export {}
