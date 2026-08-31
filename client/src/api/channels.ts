// REST client for the SimLive-Go playout-channel API.
//
// Wire shape mirrors `swagger.yaml` exactly. All requests go through a single
// `apiFetch<T>` helper that derives the origin from `VITE_API_BASE` (which
// is configured to the `/live` path prefix in dev/prod). For the REST surface
// we strip the trailing `/live` so the same env var drives both HLS and REST.

export type ChannelStatus = "ACTIVE" | "INACTIVE" | "ERROR";

export interface PlayoutChannel {
  id: string;
  name: string;
  base_name: string;
  content_timezone: string;
  channel_epg_id: string;
  status: ChannelStatus;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface ListChannelsResp {
  data: PlayoutChannel[];
  total: number;
  page: number;
  page_size: number;
}

export interface ESpxChannelListItem {
  id: string;
  name: string;
  base_name: string;
  content_timezone: string;
}

export interface ESpxChannelsListResp {
  data: ESpxChannelListItem[];
  registered: Record<string, boolean>;
}

export interface CreateChannelReq {
  channel_id: string;
}

export interface ListChannelsParams {
  page?: number;
  pageSize?: number;
  status?: ChannelStatus;
  isActive?: boolean;
  q?: string;
}

// --- Origin resolution ---------------------------------------------------
//
// `VITE_API_BASE` is configured as the *path-prefix* of the HLS proxy
// (e.g. `http://localhost:8080/live` in dev, `/live` in prod). We strip
// the trailing `/live` to derive the REST origin. Falls back to
// `VITE_API_ORIGIN` (if set) and finally to `window.location.origin`.

function deriveOrigin(): string {
  const base = import.meta.env.VITE_API_BASE ?? "";
  const explicit = import.meta.env.VITE_API_ORIGIN ?? "";

  if (explicit) return stripTrailingSlash(explicit);

  if (typeof window !== "undefined" && base.startsWith("/")) {
    // Relative path (e.g. `/live`) — use the current origin.
    return stripTrailingSlash(window.location.origin);
  }

  if (base) {
    // Absolute URL — strip the trailing `/live` (or any path) and keep just
    // the origin. `new URL` handles the parsing for us.
    try {
      const u = new URL(base);
      return stripTrailingSlash(u.origin);
    } catch {
      /* fall through */
    }
  }

  if (typeof window !== "undefined") {
    return stripTrailingSlash(window.location.origin);
  }
  return "";
}

function stripTrailingSlash(s: string): string {
  return s.endsWith("/") ? s.slice(0, -1) : s;
}

const API_ORIGIN = deriveOrigin();

function apiUrl(path: string): string {
  const normalized = path.startsWith("/") ? path : `/${path}`;
  return `${API_ORIGIN}${normalized}`;
}

// --- HTTP helper ---------------------------------------------------------

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const res = await fetch(apiUrl(path), {
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      ...(init.headers ?? {}),
    },
    ...init,
  });

  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body?.error) msg = body.error;
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg);
  }

  // 204 No Content — return undefined cast.
  if (res.status === 204) return undefined as T;

  return (await res.json()) as T;
}

// --- Endpoint wrappers ---------------------------------------------------

export function listChannels(
  params: ListChannelsParams = {},
): Promise<ListChannelsResp> {
  const qs = new URLSearchParams();
  if (params.page != null) qs.set("page", String(params.page));
  if (params.pageSize != null) qs.set("page_size", String(params.pageSize));
  if (params.status) qs.set("status", params.status);
  if (params.isActive != null)
    qs.set("is_active", params.isActive ? "true" : "false");
  if (params.q) qs.set("q", params.q);

  const search = qs.toString();
  return apiFetch<ListChannelsResp>(
    `/api/channels${search ? `?${search}` : ""}`,
  );
}

export function listESpxChannels(): Promise<ESpxChannelsListResp> {
  return apiFetch<ESpxChannelsListResp>(`/api/channels/esp`);
}

export function getChannel(id: string): Promise<PlayoutChannel> {
  return apiFetch<PlayoutChannel>(`/api/channels/${encodeURIComponent(id)}`);
}

export function startChannel(id: string): Promise<PlayoutChannel> {
  return apiFetch<PlayoutChannel>(
    `/api/channels/${encodeURIComponent(id)}/start`,
    { method: "POST" },
  );
}

export function stopChannel(id: string): Promise<PlayoutChannel> {
  return apiFetch<PlayoutChannel>(
    `/api/channels/${encodeURIComponent(id)}/stop`,
    { method: "POST" },
  );
}

export function createChannel(req: CreateChannelReq): Promise<{
  message: string;
  data: PlayoutChannel;
}> {
  return apiFetch(`/api/channels`, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

// --- HLS URL helper ------------------------------------------------------
//
// Given a channel id, returns the master playlist URL on the same origin
// the REST API lives on. The dev/prod proxy maps `/live/*` to the backend.

export function hlsUrlFor(channelId: string): string {
  return `${API_ORIGIN}/live/${channelId}/playlist.m3u8`;
}

export { API_ORIGIN };
