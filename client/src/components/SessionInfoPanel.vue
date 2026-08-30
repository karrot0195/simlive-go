<script setup lang="ts">
import { onUnmounted, computed } from "vue";
import { UIElement, createPlayer } from "@videojs/html";
import {
  liveVideoFeatures,
  streamTypeFeature,
  qualityFeature,
  selectStreamType,
  selectPlayback,
  selectQuality,
  selectVolume,
} from "@videojs/core/dom";
import {
  sessionInfoStore,
  type Segment,
} from "./sessionInfoStore.ts";

// --- Watch-time tracker ---
//
// The custom element runs a `requestAnimationFrame` loop. We accumulate the
// delta between frames (in seconds) only while the player is not paused. The
// rAF loop is throttled by the browser when the tab is backgrounded, so
// background time is effectively excluded.

class SessionInfoPanelElement extends UIElement {
  static readonly tagName = "session-info-panel";

  readonly #streamType = new PlayerController(this, selectStreamType);
  readonly #playback = new PlayerController(this, selectPlayback);
  readonly #quality = new PlayerController(this, selectQuality);
  readonly #volume = new PlayerController(this, selectVolume);

  #rafHandle: number | null = null;
  #lastTickAt: number | null = null;

  override connectedCallback(): void {
    super.connectedCallback();
    sessionInfoStore.ready = true;
    this.#startWatchTimeLoop();
  }

  override disconnectedCallback(): void {
    this.#stopWatchTimeLoop();
    super.disconnectedCallback();
  }

  #startWatchTimeLoop(): void {
    if (this.#rafHandle !== null) return;
    const tick = (now: number) => {
      const last = this.#lastTickAt;
      this.#lastTickAt = now;
      if (last !== null && !sessionInfoStore.paused) {
        const deltaSec = (now - last) / 1000;
        if (deltaSec > 0 && deltaSec < 1) {
          sessionInfoStore.watchTimeSeconds += deltaSec;
        }
      }
      this.#rafHandle = requestAnimationFrame(tick);
    };
    this.#rafHandle = requestAnimationFrame(tick);
  }

  #stopWatchTimeLoop(): void {
    if (this.#rafHandle !== null) {
      cancelAnimationFrame(this.#rafHandle);
      this.#rafHandle = null;
    }
    this.#lastTickAt = null;
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

    const quality = this.#quality.value;
    if (quality) {
      sessionInfoStore.renditionList = quality.videoRenditionList ?? [];
      sessionInfoStore.activeRendition = quality.activeVideoRendition ?? null;
    }

    const volume = this.#volume.value;
    if (volume) {
      sessionInfoStore.muted = volume.muted ?? false;
      sessionInfoStore.volume = volume.volume ?? 1;
    }
  }
}

// --- Player controller setup ---

const { PlayerController } = createPlayer({
  features: [...liveVideoFeatures, streamTypeFeature, qualityFeature],
});

if (!customElements.get(SessionInfoPanelElement.tagName)) {
  customElements.define(SessionInfoPanelElement.tagName, SessionInfoPanelElement);
}

onUnmounted(() => {
  sessionInfoStore.ready = false;
});

// --- Computed values + formatters ---

const watchTimeFormatted = computed(() => formatTime(sessionInfoStore.watchTimeSeconds));

const activeRenditionKey = computed(() => sessionInfoStore.activeRendition?.id ?? null);

// Segments are appended in playback order (oldest first). Display in
// reverse-chronological order so the most recent segment is at the top.
const segmentsReversed = computed<Segment[]>(() =>
  sessionInfoStore.segments.slice().reverse(),
);

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

function formatClock(epochMs: number): string {
  if (!epochMs || Number.isNaN(epochMs)) return "—";
  const d = new Date(epochMs);
  return d.toISOString().substring(11, 19) + "Z";
}

function basename(path: string): string {
  if (!path) return "—";
  try {
    const u = new URL(path, window.location.href);
    return u.pathname.split("/").pop() || u.pathname;
  } catch {
    return path.split("/").pop() || path;
  }
}

function variantLabel(): string {
  const r = sessionInfoStore.activeRendition;
  if (!r) return "—";
  if (r.width && r.height) return `${r.width}×${r.height}`;
  return r.id ?? "—";
}
</script>

<template>
  <session-info-panel
    class="block w-full h-full overflow-y-auto bg-gradient-to-b from-slate-950 via-slate-900 to-slate-950 text-slate-100"
  >
    <div class="p-5 space-y-5">
      <!-- Header card: live status + watch time -->
      <section
        class="rounded-xl border border-slate-800 bg-slate-900/60 backdrop-blur-sm p-4 shadow-lg shadow-black/40"
      >
        <div class="flex items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <span
              class="inline-block w-2.5 h-2.5 rounded-full"
              :class="{
                'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.8)] animate-pulse':
                  sessionInfoStore.streamType === 'live',
                'bg-blue-500':
                  sessionInfoStore.streamType === 'on-demand',
                'bg-yellow-500':
                  sessionInfoStore.streamType === 'unknown',
              }"
            ></span>
            <h2 class="text-sm font-semibold tracking-wide uppercase text-slate-100">
              {{
                sessionInfoStore.streamType === "live"
                  ? "Live"
                  : sessionInfoStore.streamType === "on-demand"
                    ? "On Demand"
                    : "Unknown"
              }}
            </h2>
          </div>
          <div class="text-right">
            <div class="text-[10px] uppercase tracking-widest text-slate-400">
              Watching
            </div>
            <div class="text-lg font-mono font-semibold text-emerald-300 tabular-nums">
              {{ watchTimeFormatted }}
            </div>
          </div>
        </div>

        <div
          v-if="sessionInfoStore.inAdBreak"
          class="mt-3 flex items-center gap-2 rounded-md border border-rose-500/60 bg-rose-500/10 px-3 py-2 text-xs font-semibold text-rose-200"
        >
          <span class="inline-block w-2 h-2 rounded-full bg-rose-500 animate-pulse"></span>
          AD BREAK
        </div>
      </section>

      <!-- Status grid: playback + audio -->
      <section class="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
        <h3
          class="text-[10px] uppercase tracking-widest text-slate-400 mb-3 font-semibold"
        >
          Status
        </h3>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-xs font-mono">
          <dt class="text-slate-400">Playback</dt>
          <dd
            class="text-right"
            :class="{
              'text-emerald-400': !sessionInfoStore.paused,
              'text-slate-300': sessionInfoStore.paused,
            }"
          >
            {{ sessionInfoStore.paused ? "paused" : "playing" }}
          </dd>

          <dt class="text-slate-400">Audio</dt>
          <dd
            class="text-right"
            :class="{
              'text-rose-400': sessionInfoStore.muted,
              'text-emerald-400':
                !sessionInfoStore.muted && sessionInfoStore.volume > 0,
            }"
          >
            {{
              sessionInfoStore.muted
                ? "muted"
                : `${Math.round(sessionInfoStore.volume * 100)}%`
            }}
          </dd>
        </dl>
      </section>

      <!-- Active rendition -->
      <section class="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
        <h3
          class="text-[10px] uppercase tracking-widest text-slate-400 mb-3 font-semibold"
        >
          Active rendition
        </h3>
        <div
          v-if="sessionInfoStore.activeRendition"
          class="rounded-lg border border-emerald-500/40 bg-emerald-500/5 p-3"
        >
          <div class="flex items-baseline justify-between">
            <div
              class="text-2xl font-mono font-bold text-emerald-300 tabular-nums"
            >
              {{
                sessionInfoStore.activeRendition.width &&
                sessionInfoStore.activeRendition.height
                  ? `${sessionInfoStore.activeRendition.width}×${sessionInfoStore.activeRendition.height}`
                  : variantLabel()
              }}
            </div>
            <div
              class="px-2 py-0.5 rounded-full bg-emerald-500/20 text-emerald-300 text-[10px] font-semibold uppercase tracking-wider"
            >
              Playing
            </div>
          </div>
          <div class="mt-2 text-xs font-mono text-slate-300">
            {{ formatBitrate(sessionInfoStore.activeRendition.bitrate) }}
          </div>
        </div>
        <p v-else class="text-xs text-slate-500">No active rendition yet.</p>
      </section>

      <!-- Available renditions (with strong CUE IN highlight on the playing one) -->
      <section class="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
        <h3
          class="text-[10px] uppercase tracking-widest text-slate-400 mb-3 font-semibold flex items-center justify-between"
        >
          <span>Available renditions</span>
          <span
            class="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-300"
          >
            {{ sessionInfoStore.renditionList.length }}
          </span>
        </h3>
        <ul
          v-if="sessionInfoStore.renditionList.length"
          class="space-y-1.5"
        >
          <li
            v-for="r in sessionInfoStore.renditionList"
            :key="r.id ?? `${r.width}x${r.height}`"
            class="flex items-center justify-between text-xs font-mono rounded-md px-2.5 py-1.5 transition"
            :class="{
              'bg-emerald-500/15 border border-emerald-500/50 text-emerald-200':
                activeRenditionKey === r.id,
              'bg-slate-800/40 border border-transparent text-slate-400':
                activeRenditionKey !== r.id,
            }"
          >
            <span class="flex items-center gap-2">
              <span
                v-if="activeRenditionKey === r.id"
                class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.8)]"
                aria-hidden="true"
              ></span>
              <span
                v-else
                class="inline-block w-1.5 h-1.5 rounded-full bg-slate-600"
                aria-hidden="true"
              ></span>
              <span>
                {{
                  r.width && r.height
                    ? `${r.width}×${r.height}`
                    : r.id ?? "—"
                }}
              </span>
            </span>
            <span class="flex items-center gap-2">
              <span>{{ formatBitrate(r.bitrate) }}</span>
            </span>
          </li>
        </ul>
        <p v-else class="text-xs text-slate-500">No renditions reported yet.</p>
      </section>

      <!-- Segments timeline: every played segment with its timestamp and a
           highlight when the segment falls inside a SCTE-35 CUE-OUT/CUE-IN
           range. -->
      <section class="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
        <h3
          class="text-[10px] uppercase tracking-widest text-slate-400 mb-3 font-semibold flex items-center justify-between"
        >
          <span>Segments</span>
          <span
            class="rounded-full bg-slate-800 px-2 py-0.5 text-[10px] text-slate-300"
          >
            {{ sessionInfoStore.segments.length }}
          </span>
        </h3>
        <div
          v-if="sessionInfoStore.segments.length"
          class="segments-scroll max-h-[28rem] overflow-y-auto pr-2"
        >
          <ul class="space-y-1">
            <li
              v-for="seg in segmentsReversed"
              :key="seg.sn"
              class="grid grid-cols-[auto_1fr_auto] items-center gap-2 text-xs font-mono rounded-md px-2.5 py-1.5"
              :class="{
                'bg-rose-500/10 border border-rose-500/40 text-rose-100':
                  seg.cueRange?.type === 'out',
                'bg-sky-500/10 border border-sky-500/40 text-sky-100':
                  seg.cueRange?.type === 'in',
                'bg-slate-800/40 border border-transparent text-slate-300':
                  !seg.cueRange,
              }"
              :title="seg.url"
            >
              <!-- Wall-clock time of the segment start -->
              <span
                class="text-[10px] tabular-nums"
                :class="{
                  'text-rose-300/80': seg.cueRange?.type === 'out',
                  'text-sky-300/80': seg.cueRange?.type === 'in',
                  'text-slate-500': !seg.cueRange,
                }"
              >
                {{ seg.programDateTime ? formatClock(seg.programDateTime) : "—" }}
              </span>

              <!-- Segment basename + sequence number -->
              <span class="truncate">
                <span class="text-slate-500 mr-1.5">#{{ seg.sn }}</span>
                <span>{{ basename(seg.url) }}</span>
              </span>

              <!-- Cue marker pill (always shown when the segment is in a cue
                   range; otherwise the segment duration). -->
              <span
                v-if="seg.cueRange"
                class="text-[9px] font-bold tracking-wider uppercase px-1.5 py-0.5 rounded whitespace-nowrap"
                :class="{
                  'bg-rose-500/30 text-rose-100':
                    seg.cueRange.type === 'out',
                  'bg-sky-500/30 text-sky-100':
                    seg.cueRange.type === 'in',
                }"
              >
                CUE {{ seg.cueRange.type === "out" ? "OUT" : "IN" }}
              </span>
              <span v-else class="text-[10px] text-slate-500 tabular-nums">
                {{ seg.duration.toFixed(2) }}s
              </span>
            </li>
          </ul>
        </div>
        <p v-else class="text-xs text-slate-500">
          No segments have been decoded yet.
        </p>
      </section>
    </div>
  </session-info-panel>
</template>

<style scoped>
/* Visible scrollbar for the segments timeline. */
.segments-scroll {
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.45) transparent;
}

.segments-scroll::-webkit-scrollbar {
  width: 8px;
}

.segments-scroll::-webkit-scrollbar-track {
  background: transparent;
}

.segments-scroll::-webkit-scrollbar-thumb {
  background-color: rgba(148, 163, 184, 0.45);
  border-radius: 9999px;
  border: 2px solid transparent;
  background-clip: padding-box;
}

.segments-scroll::-webkit-scrollbar-thumb:hover {
  background-color: rgba(148, 163, 184, 0.7);
}
</style>
