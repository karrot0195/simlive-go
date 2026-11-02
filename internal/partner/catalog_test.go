package partner

import (
	"testing"
	"time"
)

func TestResolveSourcePath(t *testing.T) {
	raw := map[string]any{
		"base_name": "news",
		"logo":      map[string]any{"url": "http://cdn/logo.png"},
		"programme": map[string]any{"name": "Morning News"},
		"list":      []any{map[string]any{"field": "first"}},
	}

	if v, ok := ResolveSourcePath(raw, "base_name"); !ok || v != "news" {
		t.Fatalf("top-level path failed: %v %v", v, ok)
	}
	if v, ok := ResolveSourceString(raw, "logo.url"); !ok || v != "http://cdn/logo.png" {
		t.Fatalf("nested path failed: %v %v", v, ok)
	}
	if v, ok := ResolveSourceString(raw, "programme.name"); !ok || v != "Morning News" {
		t.Fatalf("programme path failed: %v %v", v, ok)
	}
	if v, ok := ResolveSourceString(raw, "list.0.field"); !ok || v != "first" {
		t.Fatalf("indexed path failed: %v %v", v, ok)
	}
	if _, ok := ResolveSourcePath(raw, "missing.path"); ok {
		t.Fatal("expected missing path to be absent")
	}
}

func TestNormalizeImageCandidates(t *testing.T) {
	candidates := []string{"img", "image", "logo", "thumbnail", "poster"}

	if got := normalizeImage(map[string]any{"img": "http://cdn/a.png"}, candidates); got != "http://cdn/a.png" {
		t.Fatalf("img candidate = %q", got)
	}
	if got := normalizeImage(map[string]any{"image": "http://cdn/b.png"}, candidates); got != "http://cdn/b.png" {
		t.Fatalf("image candidate = %q", got)
	}
	if got := normalizeImage(map[string]any{"logo": map[string]any{"url": "http://cdn/c.png"}}, candidates); got != "http://cdn/c.png" {
		t.Fatalf("logo.url candidate = %q", got)
	}
	if got := normalizeImage(map[string]any{"poster": map[string]any{"src": "http://cdn/d.png"}}, candidates); got != "http://cdn/d.png" {
		t.Fatalf("poster.src candidate = %q", got)
	}
	if got := normalizeImage(map[string]any{"name": "x"}, candidates); got != "" {
		t.Fatalf("expected empty image, got %q", got)
	}
}

func TestPrimaryStreamID(t *testing.T) {
	streams := []any{
		map[string]any{"stream_id": "trailer", "is_vod": true, "is_trailer": true},
		map[string]any{"stream_id": "main", "is_vod": true, "is_trailer": false},
	}
	if id, ok := PrimaryStreamID(streams); !ok || id != "main" {
		t.Fatalf("vod/trailer selection = %q %v", id, ok)
	}

	def := []any{map[string]any{"stream_id": "dflt", "name": "Default Stream"}}
	if id, ok := PrimaryStreamID(def); !ok || id != "dflt" {
		t.Fatalf("default stream selection = %q %v", id, ok)
	}

	if _, ok := PrimaryStreamID([]any{}); ok {
		t.Fatal("expected no stream")
	}

	empty := []any{map[string]any{"stream_id": "", "is_vod": true, "is_trailer": false}}
	if _, ok := PrimaryStreamID(empty); ok {
		t.Fatal("expected stream with empty id to be rejected")
	}
}

func TestFormatEPGTime(t *testing.T) {
	tm := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	got := FormatEPGTime(tm, "Asia/Bangkok")
	if got != "2026-11-02T07:00:00+07:00" {
		t.Fatalf("FormatEPGTime = %q", got)
	}
	// Invalid tz falls back to host-local; result must still be valid RFC3339.
	got = FormatEPGTime(tm, "Not/AZone")
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("fallback result not RFC3339: %q (%v)", got, err)
	}
}

func TestNormalizeChannelImgURL(t *testing.T) {
	raw := map[string]any{"id": "ch-1", "name": "One", "img_url": "https://cdn/img_url.png"}
	v, ok := NormalizeChannel(raw, &PartnerProfile{})
	if !ok {
		t.Fatal("expected channel to normalize")
	}
	if v.Img != "https://cdn/img_url.png" {
		t.Fatalf("img_url candidate = %q", v.Img)
	}
}

func TestNormalizeChannel(t *testing.T) {
	prof := &PartnerProfile{}
	raw := map[string]any{"id": "ch-1", "name": "One", "img": "http://cdn/1.png"}
	v, ok := NormalizeChannel(raw, prof)
	if !ok {
		t.Fatal("expected channel to normalize")
	}
	if v.ID != "ch-1" || v.Name != "One" || v.Img != "http://cdn/1.png" {
		t.Fatalf("unexpected view: %+v", v)
	}

	if _, ok := NormalizeChannel(map[string]any{"name": "no id"}, prof); ok {
		t.Fatal("expected channel without id to be dropped")
	}
}

func TestNormalizeEPGItem(t *testing.T) {
	raw := map[string]any{
		"local_date_start": "2026-11-02 00:00:00.000",
		"local_date_end":   "2026-11-02 01:00:00.000",
		"programme": map[string]any{
			"is_ads": false,
			"programmeStreams": []any{
				map[string]any{"stream_id": "s1", "is_vod": true, "is_trailer": false},
			},
		},
	}
	v, ok := NormalizeEPGItem(raw, "ch-1", "Asia/Bangkok")
	if !ok {
		t.Fatal("expected item to normalize")
	}
	if v.StreamID != "s1" || v.Ads {
		t.Fatalf("unexpected item: %+v", v)
	}
	if v.Start != "2026-11-02T00:00:00+07:00" {
		t.Fatalf("start = %q", v.Start)
	}

	// No primary stream → omitted.
	noStream := map[string]any{
		"local_date_start": "2026-11-02 00:00:00.000",
		"local_date_end":   "2026-11-02 01:00:00.000",
		"programme":        map[string]any{"programmeStreams": []any{}},
	}
	if _, ok := NormalizeEPGItem(noStream, "ch-1", "Asia/Bangkok"); ok {
		t.Fatal("expected item without a primary stream to be omitted")
	}
}
