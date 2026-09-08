// Typed wrapper around the `/api/channels` REST surface described in
// `swagger.yaml`. Each method corresponds to one `operationId`.
//
// The shared `client` axios instance from `@/libs/client` has
// `baseURL: API` (e.g. `http://localhost:8080/api`). Every path below
// is relative to that base, so the `listChannels` URL is
// `${API}/channels?…` — i.e. `http://localhost:8080/api/channels?…`.

import type { AxiosRequestConfig } from 'axios'
import { client } from '@/libs/client'
import type {
  CreateChannelReq,
  ESpxChannelsListResp,
  ListChannelsParams,
  ListChannelsResp,
  PlayoutChannel,
  UpdateChannelReq
} from '@/types/api'

const buildParams = (params: ListChannelsParams | undefined): Record<string, unknown> => {
  if (!params) return {}
  const out: Record<string, unknown> = {}
  if (params.page !== undefined) out.page = params.page
  if (params.page_size !== undefined) out.page_size = params.page_size
  if (params.status !== undefined) out.status = params.status
  if (params.is_active !== undefined) out.is_active = params.is_active
  if (params.q !== undefined) out.q = params.q
  return out
}

const dataOf = <T>(resp: { data: unknown }): T => resp.data as T

const unwrap = <T>(resp: { data: unknown }): T => {
  const body = resp.data as { data?: T } | T
  if (body && typeof body === 'object' && 'data' in body && body.data !== undefined) {
    return body.data as T
  }
  return body as T
}

export const api = {
  /** GET /api/channels — paginated, filterable list. */
  async listChannels(params?: ListChannelsParams, config?: AxiosRequestConfig) {
    const resp = await client.get<unknown>('/channels', {
      ...config,
      params: buildParams(params)
    })
    return dataOf<ListChannelsResp>(resp)
  },

  /** POST /api/channels — create or upsert a playout channel. */
  async createOrUpsertChannel(req: CreateChannelReq, config?: AxiosRequestConfig) {
    const resp = await client.post<unknown>('/channels', req, config)
    return unwrap<{ message: string; data: PlayoutChannel }>(resp)
  },

  /** GET /api/channels/{id} — single channel. */
  async getChannel(id: string, config?: AxiosRequestConfig) {
    const resp = await client.get<unknown>(`/channels/${encodeURIComponent(id)}`, config)
    return dataOf<PlayoutChannel>(resp)
  },

  /** PUT /api/channels/{id} — full update. */
  async updateChannel(id: string, req: UpdateChannelReq, config?: AxiosRequestConfig) {
    const resp = await client.put<unknown>(`/channels/${encodeURIComponent(id)}`, req, config)
    return dataOf<PlayoutChannel>(resp)
  },

  /** PATCH /api/channels/{id} — partial update. */
  async patchChannel(id: string, req: UpdateChannelReq, config?: AxiosRequestConfig) {
    const resp = await client.patch<unknown>(`/channels/${encodeURIComponent(id)}`, req, config)
    return dataOf<PlayoutChannel>(resp)
  },

  /** DELETE /api/channels/{id} — soft delete. */
  async deleteChannel(id: string, config?: AxiosRequestConfig): Promise<void> {
    await client.delete<unknown>(`/channels/${encodeURIComponent(id)}`, config)
  },

  /** POST /api/channels/{id}/start — flip is_active=true, status=PREPARE. */
  async startChannel(id: string, config?: AxiosRequestConfig) {
    const resp = await client.post<unknown>(
      `/channels/${encodeURIComponent(id)}/start`,
      undefined,
      config
    )
    return dataOf<PlayoutChannel>(resp)
  },

  /** POST /api/channels/{id}/stop — flip is_active=false, status=INACTIVE. */
  async stopChannel(id: string, config?: AxiosRequestConfig) {
    const resp = await client.post<unknown>(
      `/channels/${encodeURIComponent(id)}/stop`,
      undefined,
      config
    )
    return dataOf<PlayoutChannel>(resp)
  },

  /** GET /api/channels/esp — ESPX upstream list + registered map. */
  async listESpxChannels(config?: AxiosRequestConfig) {
    const resp = await client.get<unknown>('/channels/esp', config)
    return dataOf<ESpxChannelsListResp>(resp)
  }
}
