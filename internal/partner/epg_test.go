package partner

import (
	"bytes"
	"strings"
	"testing"
)

func TestEPGDocumentMetadataGating(t *testing.T) {
	items := []EPGItemView{
		{Start: "2026-11-02T00:00:00+07:00", Stop: "2026-11-02T01:00:00+07:00", StreamID: "s1", Raw: map[string]any{
			"programme": map[string]any{"name": "Morning News"},
		}},
	}
	fields := []XMLField{{Name: "title", Source: "programme.name", When: "metadata"}}

	var without bytes.Buffer
	if err := Encode(&without, EPGDocument("ch-1", items, fields, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(without.String(), "<title>") {
		t.Fatalf("metadata field should be absent without flag: %s", without.String())
	}

	var with bytes.Buffer
	if err := Encode(&with, EPGDocument("ch-1", items, fields, true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(with.String(), "<title>Morning News</title>") {
		t.Fatalf("metadata field should appear with flag: %s", with.String())
	}
}

func TestNormalizeEPGItemAdsLowercase(t *testing.T) {
	raw := map[string]any{
		"local_date_start": "2026-11-02 01:00:00.000",
		"local_date_end":   "2026-11-02 01:00:30.000",
		"programme": map[string]any{
			"is_ads": true,
			"programmeStreams": []any{
				map[string]any{"stream_id": "ads-1", "is_vod": true, "is_trailer": false},
			},
		},
	}
	v, ok := NormalizeEPGItem(raw, "ch-1", "Asia/Bangkok")
	if !ok {
		t.Fatal("expected ads item to normalize")
	}
	if !v.Ads || v.StreamID != "ads-1" {
		t.Fatalf("unexpected ads item: %+v", v)
	}
	var buf bytes.Buffer
	if err := Encode(&buf, EPGDocument("ch-1", []EPGItemView{v}, nil, false)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "<ads>true</ads>") {
		t.Fatalf("expected lowercase ads: %s", buf.String())
	}
}

func TestNormalizeEPGItemChannelOffset(t *testing.T) {
	raw := map[string]any{
		"local_date_start": "2026-11-02 00:00:00.000",
		"local_date_end":   "2026-11-02 01:00:00.000",
		"programme": map[string]any{
			"programmeStreams": []any{
				map[string]any{"stream_id": "s1", "is_vod": true, "is_trailer": false},
			},
		},
	}
	v, ok := NormalizeEPGItem(raw, "ch-1", "Asia/Bangkok")
	if !ok {
		t.Fatal("expected item to normalize")
	}
	if v.Start != "2026-11-02T00:00:00+07:00" || v.Stop != "2026-11-02T01:00:00+07:00" {
		t.Fatalf("unexpected start/stop: %q %q", v.Start, v.Stop)
	}
}
