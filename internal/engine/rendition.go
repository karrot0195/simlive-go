package engine

import (
	"fmt"
	"sort"
	"strings"

	"simlive/internal/utils"
)

const DefaultRendition = "360p"

const renditionMetadataKey = "simlive:renditions:%s"

var ActiveLadder = []RenditionMeta{
	{Rendition: "360p", VodDir: "L_0", AdsDir: "L2", Width: 640, Height: 360, Bandwidth: 800_000},
	{Rendition: "720p", VodDir: "L_1", AdsDir: "L1", Width: 1280, Height: 720, Bandwidth: 2_500_000},
	{Rendition: "1080p", VodDir: "L_2", AdsDir: "L0", Width: 1920, Height: 1080, Bandwidth: 5_000_000},
}

type RenditionMeta struct {
	Rendition string `json:"rendition"`
	VodDir    string `json:"vod_dir"`
	AdsDir    string `json:"ads_dir"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Bandwidth int64  `json:"bandwidth"`
}

func resolveRung(label string) string {
	if label == "" {
		return DefaultRendition
	}
	return label
}

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

func pickRenditionURIs(metas []RenditionMeta) []string {
	out := make([]string, len(metas))
	for i, m := range metas {
		out[i] = fmt.Sprintf("%s/playlist.m3u8", resolveRung(m.Rendition))
	}
	return out
}

func allowedRenditionLabels() []string {
	raw := utils.GetEnv("ACTIVE_RENDITIONS", "360,720,1080")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p+"p")
		_ = i
	}
	if len(out) == 0 {
		out = []string{"360p", "720p", "1080p"}
	}
	return out
}

func sortRungsByLadder(metas []RenditionMeta) []RenditionMeta {
	order := map[string]int{}
	for i, rung := range allowedRenditionLabels() {
		order[rung] = i
	}

	out := make([]RenditionMeta, 0, len(metas))
	for _, m := range metas {
		if _, ok := order[m.Rendition]; ok {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return order[out[i].Rendition] < order[out[j].Rendition]
	})
	return out
}

func ladderRank(label string, order map[string]int) int {
	if r, ok := order[label]; ok {
		return r
	}
	return len(order)
}

func channelDir(tmpDir, channelID string) string {
	return fmt.Sprintf("%s/%s", tmpDir, channelID)
}

func rungDir(tmpDir, channelID, rendition string) string {
	return fmt.Sprintf("%s/%s/%s", tmpDir, channelID, resolveRung(rendition))
}

func renditionMediaRedisKey(channelID, rendition string) string {
	return fmt.Sprintf("simlive:hls:%s:rendition:%s", channelID, resolveRung(rendition))
}

func masterRedisKey(channelID string) string {
	return fmt.Sprintf("simlive:hls:%s", channelID)
}

func renditionMetadataRedisKey(channelID string) string {
	return fmt.Sprintf(renditionMetadataKey, channelID)
}
