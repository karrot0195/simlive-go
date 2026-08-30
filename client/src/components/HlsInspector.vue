<script setup lang="ts">
// `<hls-inspector>` bridges the underlying hls.js `Hls` instance of a sibling
// `<hlsjs-video>` into the shared Vue `reactive()` store used by
// `SessionInfoPanel.vue`.
//
// It surfaces:
//   - A rolling window of recently-played segments (with presentation time,
//     wall-clock time, and a marker indicating whether the segment falls
//     inside a SCTE-35 cue range)
//   - SCTE-35 CUE-IN / CUE-OUT markers parsed from the variant playlist
//     text (available on LevelDetails.m3u8 in the LEVEL_LOADED payload)
//   - Whether the player is currently inside an ad break
//
// The element is hidden (`display: none`); it has no DOM contribution other
// than the side effects on the shared store.

import { onMounted, onUnmounted } from "vue";
import Hls, { Events } from "hls.js";
import type {
  FragChangedData,
  LevelLoadedData,
  LevelSwitchedData,
} from "hls.js";
import {
  sessionInfoStore,
  SEGMENT_WINDOW_SIZE,
  type CuePoint,
} from "./sessionInfoStore.ts";

const REFRESH_MS = 1000;
const MAX_POLL_ATTEMPTS = 60; // ~60s

let pollHandle: ReturnType<typeof setInterval> | null = null;
let engine: Hls | null = null;
let siblingVideo: Element | null = null;
let pollAttempts = 0;

const hlsEventCleanups: Array<() => void> = [];

function detachEngine(): void {
  for (const cleanup of hlsEventCleanups) cleanup();
  hlsEventCleanups.length = 0;
  engine = null;
}

function attachEngine(hls: Hls): void {
  engine = hls;

  // LEVEL_LOADED — fires after every variant playlist fetch. Parse the
  // playlist text for SCTE-35 DATERANGE entries.
  const onLevelLoaded = (_event: Events.LEVEL_LOADED, data: LevelLoadedData) => {
    const levelDetails = data.details;
    if (!levelDetails?.m3u8) return;
    parseCuesFromPlaylistText(levelDetails.m3u8);
  };
  hls.on(Events.LEVEL_LOADED, onLevelLoaded);
  hlsEventCleanups.push(
    () => void hls.off(Events.LEVEL_LOADED, onLevelLoaded),
  );

  // LEVEL_SWITCHED — fires when ABR changes the active level. Currently
  // a no-op for the inspector.
  const onLevelSwitched = (
    _event: Events.LEVEL_SWITCHED,
    _data: LevelSwitchedData,
  ) => {
    /* no-op */
  };
  hls.on(Events.LEVEL_SWITCHED, onLevelSwitched);
  hlsEventCleanups.push(
    () => void hls.off(Events.LEVEL_SWITCHED, onLevelSwitched),
  );

  // FRAG_CHANGED — fires when the player switches to a new segment. This is
  // where we capture per-segment history. Each MediaFragment carries its
  // sequence number (sn), presentation start time (start), duration, URL,
  // and (if the playlist has PROGRAM-DATE-TIME) wall-clock programDateTime.
  const onFragChanged = (
    _event: Events.FRAG_CHANGED,
    data: FragChangedData,
  ) => {
    const f = data.frag;
    if (!f) return;
    if (typeof f.sn !== "number") return; // skip init segments
    if (!f.url) return;

    sessionInfoStore.currentSegment = f.url;

    const segment = {
      sn: f.sn,
      url: f.url,
      start: f.start,
      duration: f.duration,
      programDateTime: f.programDateTime ?? null,
      cueRange: findCueForPresentationTime(f.start),
    };

    appendSegment(segment);
  };
  hls.on(Events.FRAG_CHANGED, onFragChanged);
  hlsEventCleanups.push(
    () => void hls.off(Events.FRAG_CHANGED, onFragChanged),
  );
}

// Append a segment to the rolling window. Duplicate sequence numbers (the
// same segment can be reported twice on ABR switches) are replaced in place.
// The window is capped at SEGMENT_WINDOW_SIZE entries, oldest first.
function appendSegment(segment: {
  sn: number;
  url: string;
  start: number;
  duration: number;
  programDateTime: number | null;
  cueRange: CuePoint | null;
}): void {
  const list = sessionInfoStore.segments;
  const existingIdx = list.findIndex((s) => s.sn === segment.sn);
  if (existingIdx >= 0) {
    list[existingIdx] = segment;
    return;
  }
  list.push(segment);
  while (list.length > SEGMENT_WINDOW_SIZE) {
    list.shift();
  }
}

// Re-evaluate which existing segments fall inside a cue range. Called
// after new cues are parsed so segments that arrived before the cue get
// marked retroactively.
function recomputeCueRanges(): void {
  const list = sessionInfoStore.segments;
  for (const seg of list) {
    seg.cueRange = findCueForPresentationTime(seg.start);
  }
}

// Find the cue range that contains a given presentation time, if any.
// Cues are anchored to wall-clock time. We convert the segment's
// presentation seconds to wall-clock ms using the most recent
// programDateTime anchor, then compare against the cue range.
function findCueForPresentationTime(
  presentationStartSec: number,
): CuePoint | null {
  if (sessionInfoStore.cuePoints.length === 0) return null;

  // Find the most recent segment with a programDateTime to anchor the
  // presentation time to wall-clock time.
  const list = sessionInfoStore.segments;
  let anchor: { presSec: number; wallMs: number } | null = null;
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i].programDateTime != null) {
      anchor = { presSec: list[i].start, wallMs: list[i].programDateTime! };
      break;
    }
  }
  if (!anchor) return null;

  const wallMs =
    anchor.wallMs + (presentationStartSec - anchor.presSec) * 1000;
  for (const cue of sessionInfoStore.cuePoints) {
    if (wallMs >= cue.start && wallMs < cue.end) {
      return cue;
    }
  }
  return null;
}

// Parse an HLS playlist (master or media) and extract SCTE-35 DATERANGE
// entries that carry `SCTE35-OUT` or `SCTE35-IN` attributes.
function parseCuesFromPlaylistText(text: string): void {
  const datarangeRe = /#EXT-X-DATERANGE:([^\n]+)/g;
  const raw: Array<{
    type: "in" | "out";
    start: number;
    duration: number;
    endOnNext: boolean;
  }> = [];
  let m: RegExpExecArray | null;

  while ((m = datarangeRe.exec(text)) !== null) {
    const attrs = m[1];
    const scte35 = attrs.match(/SCTE35-(OUT|IN)=/);
    if (!scte35) continue;

    const type = scte35[1].toLowerCase() as "in" | "out";
    const startDate = attrs.match(/START-DATE="([^"]+)"/);
    const durationStr = attrs.match(/DURATION=([0-9.]+)/);
    const endOnNext = /END-ON-NEXT=YES/.test(attrs);

    const start = startDate ? Date.parse(startDate[1]) : 0;
    const duration = durationStr ? Number.parseFloat(durationStr[1]) : 0;

    if (!Number.isNaN(start)) {
      raw.push({ type, start, duration, endOnNext });
    }
  }

  if (raw.length === 0) return;

  // Resolve `end` for each cue. END-ON-NEXT=YES means the cue ends at the
  // start of the next cue (any type); otherwise the end is start+duration
  // for CUE-OUT, or equal to start for CUE-IN.
  raw.sort((a, b) => a.start - b.start);
  const resolved: CuePoint[] = raw.map((c, i) => {
    let end: number;
    if (c.endOnNext) {
      const next = raw[i + 1];
      end = next ? next.start : c.start + c.duration * 1000;
    } else {
      end = c.type === "out" ? c.start + c.duration * 1000 : c.start;
    }
    return {
      type: c.type,
      start: c.start,
      duration: c.duration,
      end,
      endOnNext: c.endOnNext,
    };
  });

  // Merge with any cues already seen, dedup by (type, start), then sort.
  const existing = sessionInfoStore.cuePoints;
  const seen = new Set<string>();
  for (const c of resolved) seen.add(`${c.type}-${c.start}`);
  const merged = [
    ...existing.filter((c) => !seen.has(`${c.type}-${c.start}`)),
    ...resolved,
  ];
  merged.sort((a, b) => a.start - b.start);
  sessionInfoStore.cuePoints = merged;

  // Re-evaluate existing segments against the (possibly expanded) cue set.
  recomputeCueRanges();

  const last = merged[merged.length - 1];
  const hasOut = merged.some((c) => c.type === "out");
  sessionInfoStore.inAdBreak = hasOut && last.type === "out";
}

function findSiblingVideo(): Element | null {
  const self = document.querySelector("hls-inspector");
  if (!self) return null;
  // Walk up to the nearest <media-container> ancestor, then search for the
  // sibling hlsjs-video from there. Falls back to a global query.
  const container = self.closest("media-container");
  if (container) {
    return container.querySelector("hlsjs-video");
  }
  return document.querySelector("hlsjs-video");
}

onMounted(() => {
  pollHandle = setInterval(() => {
    pollAttempts += 1;
    if (engine) return;
    if (!siblingVideo) siblingVideo = findSiblingVideo();
    const candidate = siblingVideo as
      | (Element & { engine?: Hls | null })
      | null;
    if (candidate?.engine) {
      attachEngine(candidate.engine);
    }
    if (pollAttempts >= MAX_POLL_ATTEMPTS && pollHandle) {
      clearInterval(pollHandle);
      pollHandle = null;
    }
  }, REFRESH_MS);
});

onUnmounted(() => {
  if (pollHandle) clearInterval(pollHandle);
  pollHandle = null;
  detachEngine();
});
</script>

<template>
  <!-- The element itself is hidden. Its only job is to wire the hls.js
       engine into the shared reactive store. -->
  <hls-inspector class="hidden" aria-hidden="true"></hls-inspector>
</template>
