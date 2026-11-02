package partner

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"simlive/internal/utils"
)

// defaultImageCandidates is the ordered list of upstream image field names used
// when a profile does not configure an explicit image source. `img_url` is the
// field exposed by the partner channel source; the rest cover common variants.
var defaultImageCandidates = []string{"img_url", "img", "image", "logo", "thumbnail", "poster"}

// ChannelView is the normalized channel handed to the XML builder.
type ChannelView struct {
	ID   string
	Name string
	Img  string
	Raw  map[string]any
}

// EPGItemView is the normalized programme handed to the XML builder.
type EPGItemView struct {
	Start    string
	Stop     string
	StreamID string
	Ads      bool
	Raw      map[string]any
}

// ResolveSourcePath walks a dotted path (with numeric indices) into raw.
func ResolveSourcePath(raw map[string]any, path string) (any, bool) {
	if raw == nil || path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var current any = raw
	for _, part := range parts {
		switch node := current.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			current = v
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			current = node[idx]
		default:
			return nil, false
		}
	}
	return current, true
}

// ResolveSourceString resolves a dotted path to a string value.
func ResolveSourceString(raw map[string]any, path string) (string, bool) {
	v, ok := ResolveSourcePath(raw, path)
	if !ok {
		return "", false
	}
	return valueToString(v), true
}

// valueToString renders scalars as text; nested objects are not stringifiable.
func valueToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return boolString(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return ""
	}
}

// NormalizeChannel maps an upstream raw channel record to a ChannelView.
// An empty id drops the item.
func NormalizeChannel(raw map[string]any, profile *PartnerProfile) (ChannelView, bool) {
	if raw == nil {
		return ChannelView{}, false
	}
	id, _ := ResolveSourceString(raw, "id")
	if id == "" {
		return ChannelView{}, false
	}
	name, _ := ResolveSourceString(raw, "name")
	if name == "" {
		name, _ = ResolveSourceString(raw, "base_name")
	}
	img := normalizeImage(raw, defaultImageCandidates)
	return ChannelView{ID: id, Name: name, Img: img, Raw: raw}, true
}

// normalizeImage resolves the first non-empty candidate to a URL string,
// unwrapping object values via a `url` or `src` field.
func normalizeImage(raw map[string]any, candidates []string) string {
	for _, key := range candidates {
		v, ok := raw[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			if t != "" {
				return t
			}
		case map[string]any:
			if s, ok := ResolveSourceString(t, "url"); ok && s != "" {
				return s
			}
			if s, ok := ResolveSourceString(t, "src"); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// PrimaryStreamID applies the planner's primary-stream rule: the first stream
// where is_vod && !is_trailer, or the stream named "Default Stream".
func PrimaryStreamID(streams []any) (string, bool) {
	for _, s := range streams {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		isVod, _ := m["is_vod"].(bool)
		isTrailer, _ := m["is_trailer"].(bool)
		name, _ := m["name"].(string)
		if isVod && !isTrailer || name == "Default Stream" {
			id, _ := m["stream_id"].(string)
			if id == "" {
				return "", false
			}
			return id, true
		}
	}
	return "", false
}

// NormalizeEPGItem maps an upstream schedule item to an EPGItemView. Items
// without a primary stream are omitted.
func NormalizeEPGItem(raw map[string]any, channelID, contentTimezone string) (EPGItemView, bool) {
	if raw == nil {
		return EPGItemView{}, false
	}
	streams, _ := ResolveSourcePath(raw, "programme.programmeStreams")
	streamList, _ := streams.([]any)
	streamID, ok := PrimaryStreamID(streamList)
	if !ok {
		return EPGItemView{}, false
	}

	startRaw, _ := ResolveSourceString(raw, "local_date_start")
	stopRaw, _ := ResolveSourceString(raw, "local_date_end")
	start, err := utils.ParsePartnerTime(startRaw, contentTimezone)
	if err != nil {
		return EPGItemView{}, false
	}
	stop, err := utils.ParsePartnerTime(stopRaw, contentTimezone)
	if err != nil {
		return EPGItemView{}, false
	}

	ads, _ := ResolveSourcePath(raw, "programme.is_ads")
	adsBool, _ := ads.(bool)

	return EPGItemView{
		Start:    FormatEPGTime(start, contentTimezone),
		Stop:     FormatEPGTime(stop, contentTimezone),
		StreamID: streamID,
		Ads:      adsBool,
		Raw:      raw,
	}, true
}

// FormatEPGTime renders t as RFC3339 in tz. An invalid timezone falls back to
// host-local time, then to configs.TZ.
func FormatEPGTime(t time.Time, tz string) string {
	loc := resolveLocation(tz)
	return t.In(loc).Format(time.RFC3339)
}

func resolveLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return utils.LocalLocation()
}

// String renders a view for debugging.
func (c ChannelView) String() string {
	return fmt.Sprintf("channel{id=%s name=%s img=%s}", c.ID, c.Name, c.Img)
}
