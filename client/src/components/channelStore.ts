// Shared reactive store for the channel browser UI.
//
// `VideoPlayer.vue` watches `activeChannelId` and swaps the `<hlsjs-video>`
// `src` accordingly. The ChannelListPanel writes to it when the user clicks
// "Select". A null id means "no channel selected" and the player renders
// a placeholder.
//
// The active selection is also mirrored to the URL as `?channel=<id>` so:
//   - the player auto-selects the right channel on reload / deep-link
//   - the sidebar can rehydrate the selection after the local list loads
//   - browser back/forward navigation moves between channels
// We use `history.replaceState` (rather than `pushState`) to keep the back
// button clean — selection is a UI state, not a destination.

import { reactive } from "vue";
import { hlsUrlFor } from "../api/channels.ts";
import type { PlayoutChannel } from "../api/channels.ts";

const QUERY_PARAM = "channel";

export const channelStore = reactive({
  /** Currently-selected channel. Null = no channel selected, player shows placeholder. */
  activeChannelId: null as string | null,
  /** Cached metadata for the active channel. Used by the player chrome. */
  activeChannel: null as PlayoutChannel | null,
  /** Monotonic counter bumped on every active-channel change. Components can
   *  `watch` this if they need to react to switches (rather than the id alone,
   *  which may flip from A→A during polls). */
  changeToken: 0,
  /** True once the URL → store rehydration has run. Components that need to
   *  wait for a deep-linked channel (e.g. before showing the player) can
   *  watch this. */
  hydratedFromUrl: false,
});

// --- URL <-> store ------------------------------------------------------

function readChannelFromUrl(): string | null {
  if (typeof window === "undefined") return null;
  const params = new URLSearchParams(window.location.search);
  return params.get(QUERY_PARAM);
}

function writeChannelToUrl(id: string | null): void {
  if (typeof window === "undefined") return;
  const url = new URL(window.location.href);
  if (id) {
    url.searchParams.set(QUERY_PARAM, id);
  } else {
    url.searchParams.delete(QUERY_PARAM);
  }
  // `replaceState` avoids polluting history with every selection.
  window.history.replaceState(window.history.state, "", url.toString());
}

// --- Setters -------------------------------------------------------------

export function selectChannel(channel: PlayoutChannel): void {
  channelStore.activeChannelId = channel.id;
  channelStore.activeChannel = channel;
  channelStore.changeToken += 1;
  writeChannelToUrl(channel.id);
}

export function clearActiveChannel(): void {
  channelStore.activeChannelId = null;
  channelStore.activeChannel = null;
  channelStore.changeToken += 1;
  writeChannelToUrl(null);
}

/**
 * Set just the id (no channel metadata). Used during URL rehydration when
 * the local list hasn't loaded yet — the ChannelListPanel will fill in the
 * metadata after the fetch completes. Bumps `changeToken` so the player
 * remounts when metadata arrives.
 */
export function setActiveChannelId(id: string | null): void {
  if (channelStore.activeChannelId === id) return;
  channelStore.activeChannelId = id;
  channelStore.activeChannel = null;
  channelStore.changeToken += 1;
  writeChannelToUrl(id);
}

/**
 * If the URL carries a `?channel=<id>`, return it. The caller is responsible
 * for resolving the id into a full PlayoutChannel once the list loads.
 */
export function pendingChannelFromUrl(): string | null {
  return readChannelFromUrl();
}

export function activeHlsUrl(): string | null {
  const id = channelStore.activeChannelId;
  if (!id) return null;
  return hlsUrlFor(id);
}
