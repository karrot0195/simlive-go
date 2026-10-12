# Client — EP UI

Vue 3 + TypeScript + Vite client for the simlive playout system (ES Partner)

## Stack
- Vue 3.4.29, Vuetify 3.7.2, Pinia 2.1.7, Vue Router 4.3.3
- Video.js 8.17.4 + videojs-contrib-hls 5.15.0 (HLS playback)
- Tailwind CSS 3.4.11, TypeScript ~5.4.0
- Build: Vite 5.3.1 (`pnpm` / `npm`)

## Scripts
- `pnpm dev` / `npm run dev` — dev server (`vite --host 0.0.0.0`)
- `pnpm build` / `npm run build` — type-check + build
- `pnpm preview` — preview production build
- `pnpm lint` — ESLint + fix
- `pnpm format` — Prettier

## Key Components
- `App.vue` — layout + auth provider
- `SimulatedLive.vue` — live stream panel (auto-selects `LIVE`/`PREPARE` channel)
- `PlayerLiveStream.vue` — video.js HLS player
- `StreamPanel.vue`, `ChannelEpgPanel.vue` — EPG / stream controls
- `stores/channels.ts`, `stores/auth.ts` — Pinia stores
- `libs/api.ts`, `libs/hls.ts` — API + HLS helpers

## Notes
- Uses `@espx/ui` component library.
- Player relies on the Go server's HLS master playlist (`simlive:hls:{ch}`).
