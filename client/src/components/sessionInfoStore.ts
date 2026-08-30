// Shared reactive store for the info panel. Both `HlsInspector.vue` (writes)
// and `SessionInfoPanel.vue` (reads) import the same store so the inspector
// can push hls.js-level data (variant URL, current segment, SCTE-35 cues,
// segment history) without going through the player store / PlayerController.

import { reactive } from "vue";

export type CueType = "in" | "out";

export interface CuePoint {
  type: CueType;
  /** Epoch milliseconds of the cue's START-DATE. */
  start: number;
  /** Ad duration in seconds (CUE-OUT only; 0 for CUE-IN). */
  duration: number;
  /** Resolved end epoch ms (start of the next cue, or start+duration). */
  end: number;
  /** True when DATERANGE has END-ON-NEXT=YES. */
  endOnNext: boolean;
}

export interface Segment {
  sn: number;
  url: string;
  /** Presentation start time, in seconds. */
  start: number;
  /** Segment duration, in seconds. */
  duration: number;
  /** Wall-clock time (epoch ms) if the playlist carries PROGRAM-DATE-TIME. */
  programDateTime: number | null;
  /** Cue range this segment falls inside, if any. Re-evaluated whenever a
   *  new cue is parsed so segments already in the history get marked
   *  retroactively. */
  cueRange: CuePoint | null;
}

export interface MediaVideoRenditionSummary {
  id?: string;
  width?: number;
  height?: number;
  bitrate?: number;
  selected: boolean;
}

export const SEGMENT_WINDOW_SIZE = 200;

export const sessionInfoStore = reactive({
  ready: false,
  streamType: "unknown" as "live" | "on-demand" | "unknown",
  paused: true,
  currentTime: 0,
  /** Total time the user has actively watched, in seconds (paused time is not counted). */
  watchTimeSeconds: 0,
  muted: false,
  volume: 1,

  activeRendition: null as MediaVideoRenditionSummary | null,
  renditionList: [] as Array<{
    id?: string;
    width?: number;
    height?: number;
    bitrate?: number;
  }>,

  /** Most recent segment URL fed to MSE. */
  currentSegment: "" as string,

  /** All SCTE-35 CUE-OUT / CUE-IN markers parsed so far, sorted by start. */
  cuePoints: [] as CuePoint[],
  /** True when the most recent cue is a CUE-OUT (i.e. an ad break is in progress). */
  inAdBreak: false,

  /** Sliding window of segments observed in playback order, oldest first. */
  segments: [] as Segment[],
});
