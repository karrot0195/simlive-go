package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"simlive/internal/partner"
)

// The XML EPG source returns stream_id values that include the storage bucket
// (rclone remote:bucket/... form). The planner consumes them verbatim, so the
// parser must keep the prefix even when STORAGE_BUCKET is configured.
func TestGetEPGScheduleItemsFromXMLKeepsBucketInStreamID(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <programme start="20261103000000 +0700" stop="20261103010000 +0700">
    <ads path="partner-video-lib:partner-video-lib/ads-v3/break-1" duration="30" start="20261103001000 +0700"/>
    <stream stream_id="partner-video-lib:partner-video-lib/vod/ch-1/prog-1"/>
  </programme>
</tv>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/common/channel/epg/") {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	t.Setenv("STORAGE_BUCKET", "partner-video-lib:partner-video-lib")

	c := &PartnerClient{baseURL: srv.URL, profile: &partner.PartnerProfile{}, httpClient: srv.Client()}
	resp, err := c.GetEPGScheduleItemsFromXML("ch-1", "2026-11-03", "+07:00")
	if err != nil {
		t.Fatalf("GetEPGScheduleItemsFromXML error: %v", err)
	}
	if resp == nil || len(resp.Data) != 1 {
		t.Fatalf("want 1 schedule item, got %+v", resp)
	}

	streams := resp.Data[0].Programme.ProgrammeStreams
	if len(streams) != 1 {
		t.Fatalf("want 1 programme stream, got %d", len(streams))
	}
	want := "partner-video-lib:partner-video-lib/vod/ch-1/prog-1"
	if streams[0].StreamID != want {
		t.Fatalf("StreamID = %q, want %q (bucket prefix must be kept verbatim)", streams[0].StreamID, want)
	}

	ads := resp.Data[0].Programme.Ads
	if len(ads) != 1 || ads[0].Path != "partner-video-lib:partner-video-lib/ads-v3/break-1" || ads[0].Duration != 30 {
		t.Fatalf("ads not parsed as expected: %+v", ads)
	}
}
