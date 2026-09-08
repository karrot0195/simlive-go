// Centralised HLS URL builders for the M1.5 multi-rendition surface
// implemented in `cmd/api/main.go` and documented in `API-doc.md` §4.
//
// IMPORTANT: the Go server uses the NESTED rung path (segments are
// served under `{channelId}/{rendition}/`), NOT the flat shape shown
// in `swagger.yaml` (which is outdated). The helpers below match the
// running server. If the server is later refactored to the flat
// shape, update this file to match.

import { LIVE_API } from '@/constants'

export const RENDITIONS = ['360p', '720p', '1080p'] as const
export type Rendition = (typeof RENDITIONS)[number]

const stripLeadingSlash = (s: string) => s.replace(/^\/+/, '')

/** Master playlist: `/live/{channelId}/playlist.m3u8` */
export const masterPlaylistUrl = (channelId: string): string =>
  `${LIVE_API}/${stripLeadingSlash(channelId)}/playlist.m3u8`

/** Per-rung media playlist: `/live/{channelId}/{rendition}/playlist.m3u8` */
export const rungPlaylistUrl = (channelId: string, rendition: Rendition): string =>
  `${LIVE_API}/${stripLeadingSlash(channelId)}/${rendition}/playlist.m3u8`

/** Per-rung .ts segment: `/live/{channelId}/{rendition}/{segment}` */
export const segmentUrl = (channelId: string, rendition: Rendition, segment: string): string =>
  `${LIVE_API}/${stripLeadingSlash(channelId)}/${rendition}/${stripLeadingSlash(segment)}`
