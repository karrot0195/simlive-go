package engine

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultRendition is the rescue rung used when a SegmentItem has no
// Rendition field (legacy timeline members pre-M1). Matches the planner's
// own DefaultRendition constant.
const DefaultRendition = "360p"

// renditionMetadataKey mirrors planner.renditionMetadataKey so the engine can
// read the planner's metadata without an import cycle.
const renditionMetadataKey = "simlive:renditions:%s"

// ActiveLadder is the hard-coded ladder the engine advertises when no
// per-channel metadata is in Redis yet. The bandwidth numbers are nominal --
// the engine serves the SAME seg_N.ts for all rungs (shared segment cache),
// so the bandwidth hint is informational only and does not change the
// actual download / disk cost. The three rungs let hls.js / Safari exercise
// the variant-switch code path even though only one rung is real.
//
// Order matters: low-res first, high-res last, so a player picks the lowest
// rung on first connect (matches 360p default).
var ActiveLadder = []RenditionMeta{
	{Rendition: "360p", VodDir: "L_0", AdsDir: "L2", Width: 640, Height: 360, Bandwidth: 800_000},
	{Rendition: "720p", VodDir: "L_1", AdsDir: "L1", Width: 1280, Height: 720, Bandwidth: 2_500_000},
	{Rendition: "1080p", VodDir: "L_2", AdsDir: "L0", Width: 1920, Height: 1080, Bandwidth: 5_000_000},
}

// RenditionMeta mirrors planner.RenditionMeta. Re-declared here to keep the
// engine free of any planner-package import (engine only needs the JSON
// shape; planner only writes it).
type RenditionMeta struct {
	Rendition string `json:"rendition"`
	VodDir    string `json:"vod_dir"`
	AdsDir    string `json:"ads_dir"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Bandwidth int64  `json:"bandwidth"`
}

// groupSegsByRendition splits a flat SegmentItem slice into one slice per
// rendition. Empty Rendition fields are rescued to DefaultRendition so
// legacy pre-M1 members play through the active rung (M1 = 360p). Within
// each group the original order is preserved (caller is expected to have
// sorted by Sequence already). The returned map is keyed by rendition label.
//
// M1.5 NOTE: the engine now uses ONE shared segment cache on disk
// ({TMP_DIR}/{ch}/seg_N.ts) so all rung groups reference the same files
// on disk. This function is still used to bucket segments per rung for
// the per-rung playlist candidate selection, but the actual download and
// .pts marker paths are NOT bucketed -- a single flat set of files is
// produced for all rungs.
// func groupSegsByRendition(allSegs []models.SegmentItem) map[string][]models.SegmentItem {
// 	out := make(map[string][]models.SegmentItem)
// 	for _, seg := range allSegs {
// 		rend := primaryRendition(seg.Renditions)
// 		out[rend] = append(out[rend], seg)
// 	}
// 	return out
// }

// // primaryRendition returns the first rendition label in a SegmentItem's
// // Renditions slice, or DefaultRendition if the slice is empty.
// func primaryRendition(rends []string) string {
// 	if len(rends) == 0 {
// 		return DefaultRendition
// 	}
// 	return rends[0]
// }

// resolveRung returns the rendition label used for the per-rung playlist
// filename. Defensive: returns DefaultRendition if the input is empty.
func resolveRung(label string) string {
	if label == "" {
		return DefaultRendition
	}
	return label
}

// buildMasterPlaylist renders an HLS master playlist (RFC 8216 §4.3.4.2)
// from the active rung metadata. Skips rungs that lack width / height /
// bandwidth (those cannot be advertised via STREAM-INF). The rung order
// matches the input slice order; callers should sort by desired display
// order (typically ladder order). `renditionURIs[i]` is the relative URI
// for the i-th rung's media playlist (e.g. "360p/playlist.m3u8").
//
// Output always includes `#EXT-X-INDEPENDENT-SEGMENTS` -- required for
// clean hls.js variant switching. (Promoted from M2 to M1.)
func buildMasterPlaylist(metas []RenditionMeta, renditionURIs []string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	for i, m := range metas {
		if m.Width <= 0 || m.Height <= 0 || m.Bandwidth <= 0 {
			continue
		}
		uri := ""
		if i < len(renditionURIs) {
			uri = renditionURIs[i]
		}
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n%s\n",
			m.Bandwidth, m.Width, m.Height, uri)
	}
	return b.String()
}

// pickRenditionURIs maps the input meta slice to its per-rendition media
// playlist relative URIs ("360p/playlist.m3u8", "720p/playlist.m3u8", ...).
// M3.2: each media playlist lives in its own rung subdir
// ({TMP_DIR}/{ch}/{rend}/playlist.m3u8), so the URI is "{rend}/playlist.m3u8".
func pickRenditionURIs(metas []RenditionMeta) []string {
	out := make([]string, len(metas))
	for i, m := range metas {
		out[i] = fmt.Sprintf("%s/playlist.m3u8", resolveRung(m.Rendition))
	}
	return out
}

// sortRungsByLadder returns a copy of metas sorted by quality ladder order
// (low res first -> high res last) using the standard 360p < 720p < 1080p
// labels. Unknown labels sort to the end. Pure helper so master playlist
// generation is deterministic regardless of map iteration order.
func sortRungsByLadder(metas []RenditionMeta) []RenditionMeta {
	order := map[string]int{
		"360p":  0,
		"720p":  1,
		"1080p": 2,
	}
	out := make([]RenditionMeta, len(metas))
	copy(out, metas)
	sort.SliceStable(out, func(i, j int) bool {
		return ladderRank(out[i].Rendition, order) < ladderRank(out[j].Rendition, order)
	})
	return out
}

func ladderRank(label string, order map[string]int) int {
	if r, ok := order[label]; ok {
		return r
	}
	return len(order) // unknown labels sort last
}

// channelDir returns the channel root directory. M3.2: the channel root
// holds the master playlist (playlist.m3u8) + one subdir per rendition
// ({ch}/{rend}/), each with its own media playlist + seg_N.ts files.
func channelDir(tmpDir, channelID string) string {
	return fmt.Sprintf("%s/%s", tmpDir, channelID)
}

// rungDir returns the per-rendition subdir for a channel
// (`{TMP_DIR}/{ch}/{rend}/`). M3.2: each rendition downloads its own
// `seg_{seq}.ts` files and writes its own `playlist.m3u8` here.
func rungDir(tmpDir, channelID, rendition string) string {
	return fmt.Sprintf("%s/%s/%s", tmpDir, channelID, resolveRung(rendition))
}

// renditionMediaRedisKey returns the per-rendition media playlist Redis
// key (e.g. "simlive:hls:ch1:rendition:360p"). The flat
// "simlive:hls:ch1" key is reserved for the master playlist.
func renditionMediaRedisKey(channelID, rendition string) string {
	return fmt.Sprintf("simlive:hls:%s:rendition:%s", channelID, resolveRung(rendition))
}

// masterRedisKey returns the master playlist Redis key. The API route
// (cmd/api/main.go) reads this directly.
func masterRedisKey(channelID string) string {
	return fmt.Sprintf("simlive:hls:%s", channelID)
}

// renditionMetadataRedisKey returns the key the planner writes per-channel
// rung metadata to (consumed by the engine via loadRenditionMeta).
func renditionMetadataRedisKey(channelID string) string {
	return fmt.Sprintf(renditionMetadataKey, channelID)
}

// isValidRenditionLabel returns true when the given label is a known
// ladder rung. Used by the API to gate /playlist_<rend>.m3u8 routes.
func isValidRenditionLabel(label string) bool {
	switch label {
	case "360p", "720p", "1080p":
		return true
	}
	return false
}
