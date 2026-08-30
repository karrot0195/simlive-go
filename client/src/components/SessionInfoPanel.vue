<script setup lang="ts">
import { onUnmounted, reactive, computed } from "vue";
import { UIElement, createPlayer } from "@videojs/html";
import {
  liveVideoFeatures,
  streamTypeFeature,
  qualityFeature,
  selectStreamType,
  selectTime,
  selectLive,
  selectPlayback,
  selectQuality,
} from "@videojs/core/dom";

// --- Reactive store shared between the custom element and the Vue template ---
//
// `<session-info-panel>` is a custom element that lives inside the same
// `<live-stream-player>` as the video. It subscribes to the player store via
// `PlayerController` selectors and pushes slices into this reactive object on
// every update. The Vue template renders the object.

const sessionInfoStore = reactive({
  ready: false,
  streamType: "unknown" as "live" | "on-demand" | "unknown",
  paused: true,
  currentTime: 0,
  duration: 0,
  liveEdgeStart: NaN,
  targetLiveWindow: NaN,
  activeRendition: null as null | {
    id?: string;
    width?: number;
    height?: number;
    bitrate?: number;
    frameRate?: number;
    codec?: string;
    selected: boolean;
  },
  renditionList: [] as Array<{
    id?: string;
    width?: number;
    height?: number;
    bitrate?: number;
  }>,
});

const liveEdgeLag = computed(() => {
  if (!Number.isFinite(sessionInfoStore.liveEdgeStart)) return null;
  return Math.max(0, sessionInfoStore.liveEdgeStart - sessionInfoStore.currentTime);
});

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "—";
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

function formatBitrate(bps: number | undefined): string {
  if (!bps || !Number.isFinite(bps)) return "—";
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(2)} Mbps`;
  if (bps >= 1_000) return `${(bps / 1_000).toFixed(0)} kbps`;
  return `${bps} bps`;
}

// --- Custom element definition ---
//
// `SessionInfoPanelElement` is registered exactly once on the page. It extends
// `UIElement` and uses `PlayerController` to subscribe to store slices, then
// mirrors them into the Vue reactive store.
//
// The `PlayerController` constructor needs to be the configured (feature-bound)
// one returned by `createPlayer`. We call `createPlayer` again here with the
// same feature set to get that typed controller. The returned `PlayerElement`
// is NOT registered — VideoPlayer.vue already does that — but the
// `PlayerController` is reusable: it resolves the store via the player
// context, which `<session-info-panel>` discovers from its nearest ancestor
// `<live-stream-player>`.

const { PlayerController } = createPlayer({
  features: [...liveVideoFeatures, streamTypeFeature, qualityFeature],
});

class SessionInfoPanelElement extends UIElement {
  static readonly tagName = "session-info-panel";

  readonly #streamType = new PlayerController(this, selectStreamType);
  readonly #playback = new PlayerController(this, selectPlayback);
  readonly #time = new PlayerController(this, selectTime);
  readonly #live = new PlayerController(this, selectLive);
  readonly #quality = new PlayerController(this, selectQuality);

  override connectedCallback(): void {
    super.connectedCallback();
    sessionInfoStore.ready = true;
  }

  protected override update(_changed: Map<string, unknown>): void {
    const streamType = this.#streamType.value;
    if (streamType) {
      sessionInfoStore.streamType = streamType.streamType ?? "unknown";
    }

    const playback = this.#playback.value;
    if (playback) {
      sessionInfoStore.paused = playback.paused ?? true;
    }

    const time = this.#time.value;
    if (time) {
      sessionInfoStore.currentTime = time.currentTime ?? 0;
      sessionInfoStore.duration = time.duration ?? 0;
    }

    const live = this.#live.value;
    if (live) {
      sessionInfoStore.liveEdgeStart = live.liveEdgeStart ?? NaN;
      sessionInfoStore.targetLiveWindow = live.targetLiveWindow ?? NaN;
    }

    const quality = this.#quality.value;
    if (quality) {
      sessionInfoStore.renditionList = quality.videoRenditionList ?? [];
      sessionInfoStore.activeRendition = quality.activeVideoRendition ?? null;
    }
  }
}

if (!customElements.get(SessionInfoPanelElement.tagName)) {
  customElements.define(SessionInfoPanelElement.tagName, SessionInfoPanelElement);
}

onUnmounted(() => {
  sessionInfoStore.ready = false;
});
</script>

<template>
  <session-info-panel
    class="block w-full h-full overflow-y-auto bg-gray-900 text-gray-100 font-mono text-sm"
  >
    <div class="p-6 space-y-6">
      <header>
        <h2 class="text-lg font-semibold text-white">Stream Info</h2>
        <p class="text-xs text-gray-400 mt-1">
          Live state from the shared player store
        </p>
      </header>

      <section class="space-y-2">
        <h3 class="text-xs uppercase tracking-wider text-gray-400">Status</h3>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-1">
          <dt class="text-gray-400">Stream type</dt>
          <dd
            class="text-right"
            :class="{
              'text-green-400': sessionInfoStore.streamType === 'live',
              'text-blue-400': sessionInfoStore.streamType === 'on-demand',
              'text-yellow-400': sessionInfoStore.streamType === 'unknown',
            }"
          >
            {{ sessionInfoStore.streamType }}
          </dd>

          <dt class="text-gray-400">Playback</dt>
          <dd
            class="text-right"
            :class="{
              'text-green-400': !sessionInfoStore.paused,
              'text-gray-300': sessionInfoStore.paused,
            }"
          >
            {{ sessionInfoStore.paused ? "paused" : "playing" }}
          </dd>
        </dl>
      </section>

      <section class="space-y-2">
        <h3 class="text-xs uppercase tracking-wider text-gray-400">Time</h3>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-1">
          <dt class="text-gray-400">Current</dt>
          <dd class="text-right text-white">
            {{ formatTime(sessionInfoStore.currentTime) }}
          </dd>
          <dt class="text-gray-400">Duration (live edge)</dt>
          <dd class="text-right text-white">
            {{ formatTime(sessionInfoStore.duration) }}
          </dd>
          <template v-if="liveEdgeLag !== null">
            <dt class="text-gray-400">Lag from live edge</dt>
            <dd class="text-right text-white">
              {{ formatTime(liveEdgeLag) }}
            </dd>
          </template>
          <dt class="text-gray-400">DVR window</dt>
          <dd class="text-right text-white">
            {{
              Number.isFinite(sessionInfoStore.targetLiveWindow)
                ? sessionInfoStore.targetLiveWindow === Infinity
                  ? "∞"
                  : formatTime(sessionInfoStore.targetLiveWindow)
                : "—"
            }}
          </dd>
        </dl>
      </section>

      <section class="space-y-2">
        <h3 class="text-xs uppercase tracking-wider text-gray-400">Active rendition</h3>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-1">
          <dt class="text-gray-400">Resolution</dt>
          <dd class="text-right text-white">
            {{
              sessionInfoStore.activeRendition?.width &&
              sessionInfoStore.activeRendition?.height
                ? `${sessionInfoStore.activeRendition.width}×${sessionInfoStore.activeRendition.height}`
                : "—"
            }}
          </dd>
          <dt class="text-gray-400">Bitrate</dt>
          <dd class="text-right text-white">
            {{ formatBitrate(sessionInfoStore.activeRendition?.bitrate) }}
          </dd>
          <dt class="text-gray-400">Frame rate</dt>
          <dd class="text-right text-white">
            {{
              sessionInfoStore.activeRendition?.frameRate
                ? `${sessionInfoStore.activeRendition.frameRate.toFixed(2)} fps`
                : "—"
            }}
          </dd>
          <dt class="text-gray-400">Codec</dt>
          <dd class="text-right text-white">
            {{ sessionInfoStore.activeRendition?.codec ?? "—" }}
          </dd>
        </dl>
      </section>

      <section class="space-y-2">
        <h3 class="text-xs uppercase tracking-wider text-gray-400">
          Available renditions ({{ sessionInfoStore.renditionList.length }})
        </h3>
        <ul
          v-if="sessionInfoStore.renditionList.length"
          class="space-y-1"
        >
          <li
            v-for="r in sessionInfoStore.renditionList"
            :key="r.id"
            class="flex justify-between text-xs"
            :class="{
              'text-green-400':
                sessionInfoStore.activeRendition?.id === r.id,
              'text-gray-400': sessionInfoStore.activeRendition?.id !== r.id,
            }"
          >
            <span>
              {{ r.width && r.height ? `${r.width}×${r.height}` : r.id }}
            </span>
            <span>{{ formatBitrate(r.bitrate) }}</span>
          </li>
        </ul>
        <p v-else class="text-xs text-gray-500">No renditions reported yet.</p>
      </section>
    </div>
  </session-info-panel>
</template>
