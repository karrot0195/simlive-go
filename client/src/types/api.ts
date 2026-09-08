// Hand-written TypeScript types mirroring `swagger.yaml` `components.schemas`.
// Keep these in sync with the OpenAPI source of truth.

export type PlayoutChannelStatus = 'PREPARE' | 'LIVE' | 'INACTIVE' | 'ERROR'

/** Closed set accepted by PUT / PATCH (ERROR is planner-only). */
export type PlayoutChannelStatusMutable = 'PREPARE' | 'LIVE' | 'INACTIVE'

export interface PlayoutChannel {
  id: string
  name: string
  base_name?: string
  content_timezone?: string
  channel_epg_id?: string
  status: PlayoutChannelStatus
  is_active: boolean
  created_at?: string
  updated_at?: string
}

export interface CreateChannelReq {
  channel_id: string
}

export interface UpdateChannelReq {
  name?: string
  base_name?: string
  content_timezone?: string
  channel_epg_id?: string
  is_active?: boolean
  status?: PlayoutChannelStatusMutable
}

export interface ListChannelsResp {
  data: PlayoutChannel[]
  total: number
  page: number
  page_size: number
}

export interface ListChannelsParams {
  page?: number
  page_size?: number
  status?: PlayoutChannelStatus
  is_active?: boolean
  q?: string
}

export interface ESPXChannelListItem {
  id: string
  name: string
  base_name?: string
  content_timezone?: string
}

export interface ESpxChannelsListResp {
  data: ESPXChannelListItem[]
  /** Keyed by ESPX channel id; value true if already registered locally. */
  registered: Record<string, boolean>
}

export interface ApiError {
  error: string
}
