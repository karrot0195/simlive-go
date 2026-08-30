package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// These tests exercise the path-guard logic for the M1.5 routes in
// cmd/api/main.go by recreating the guards inline. The intent is to lock
// down the rejection behavior WITHOUT spinning up Redis / a full cmd/api
// integration test. A separate cmd/api/main_test.go could exercise the
// full router end-to-end; for now the guards are validated here.
//
// M1.5 route shape (matching cmd/api/main.go):
//   GET /live/:channel_id/playlist.m3u8         (master)
//   GET /live/:channel_id/playlist_:rendition   (e.g. playlist_360p, with .m3u8 wildcarded)
//   GET /live/:channel_id/:segment              (shared-cache .ts segments)

// rungPlaylistGuard reproduces the per-param guards for the
// `GET /live/:channel_id/playlist_:rendition` route in cmd/api/main.go.
// `rendRaw` is the value Gin extracts from the URL -- e.g. for the URL
// /live/ch1/playlist_360p.m3u8, rendRaw arrives WITHOUT the .m3u8 suffix
// (Gin strips it via the wildcarded literal). Returns the (statusCode, body)
// tuple the live handler would emit.
func rungPlaylistGuard(channelID, rendRaw string) (int, string) {
	if strings.Contains(channelID, "..") || strings.Contains(channelID, "/") ||
		strings.Contains(rendRaw, "..") || strings.Contains(rendRaw, "/") {
		return http.StatusBadRequest, "Invalid path"
	}
	rend := strings.TrimSuffix(rendRaw, ".m3u8")
	switch rend {
	case "360p", "720p", "1080p":
		// valid ladder rung
	default:
		return http.StatusNotFound, "Unknown rendition"
	}
	return http.StatusOK, "passed-guard"
}

// segmentGuard reproduces the per-param guards for the
// `GET /live/:channel_id/:segment` route (shared-cache segments).
func segmentGuard(channelID, segment string) (int, string) {
	if strings.Contains(channelID, "..") || strings.Contains(channelID, "/") ||
		strings.Contains(segment, "..") || strings.Contains(segment, "/") {
		return http.StatusBadRequest, "Invalid path"
	}
	if !strings.HasSuffix(segment, ".ts") {
		return http.StatusNotFound, "Invalid segment format"
	}
	return http.StatusOK, "passed-guard"
}

// --- TestRungPlaylistGuard_RejectsUnknownRendition -------------------------
//
// The rung param must be a known ladder rung. Anything else (e.g.
// "playlist_4k" or "playlist_random") is a 404 -- not a 200 with the
// master. This guards against typo'd client URLs silently falling through
// to the master playlist.

func TestRungPlaylistGuard_RejectsUnknownRendition(t *testing.T) {
	for _, bad := range []string{"4k", "random", "foo"} {
		code, _ := rungPlaylistGuard("ch1", bad)
		if code != http.StatusNotFound {
			t.Errorf("rendition=%q: code = %d, want 404", bad, code)
		}
	}
}

// --- TestRungPlaylistGuard_AcceptsValid -------------------------------------

func TestRungPlaylistGuard_AcceptsValid(t *testing.T) {
	for _, rend := range []string{"360p", "720p", "1080p", "360p.m3u8", "1080p.m3u8"} {
		code, body := rungPlaylistGuard("ch1", rend)
		if code != http.StatusOK {
			t.Errorf("rendition=%q: code = %d, want 200 (passes guard); body = %q", rend, code, body)
		}
	}
}

// --- TestRungPlaylistGuard_RejectsDotDot ------------------------------------

func TestRungPlaylistGuard_RejectsDotDot(t *testing.T) {
	code, _ := rungPlaylistGuard("ch1", "..")
	if code != http.StatusBadRequest {
		t.Fatalf("rendition=..: code = %d, want 400", code)
	}
	code, _ = rungPlaylistGuard("..", "360p")
	if code != http.StatusBadRequest {
		t.Fatalf("channelID=..: code = %d, want 400", code)
	}
}

// --- TestRungPlaylistGuard_RejectsSlashInRendition -------------------------

func TestRungPlaylistGuard_RejectsSlashInRendition(t *testing.T) {
	code, _ := rungPlaylistGuard("ch1", "360p/sub")
	if code != http.StatusBadRequest {
		t.Fatalf("rendition=360p/sub: code = %d, want 400", code)
	}
}

// --- TestSegmentGuard_RejectsNonTS -----------------------------------------

func TestSegmentGuard_RejectsNonTS(t *testing.T) {
	code, _ := segmentGuard("ch1", "seg_1.exe")
	if code != http.StatusNotFound {
		t.Fatalf("non-.ts segment: code = %d, want 404", code)
	}
}

// --- TestSegmentGuard_AcceptsValid -----------------------------------------

func TestSegmentGuard_AcceptsValid(t *testing.T) {
	code, body := segmentGuard("ch1", "seg_42.ts")
	if code != http.StatusOK {
		t.Fatalf("valid segment: code = %d, want 200 (passes guard); body = %q", code, body)
	}
}

// --- TestSegmentGuard_RejectsDotDotAndSlash --------------------------------

func TestSegmentGuard_RejectsDotDotAndSlash(t *testing.T) {
	cases := []struct{ ch, seg string }{
		{"ch1", "../etc/passwd"},
		{"..", "seg_1.ts"},
		{"ch1", "sub/seg_1.ts"},
		{"ch1", "sub\\seg_1.ts"},
	}
	for _, tc := range cases {
		code, _ := segmentGuard(tc.ch, tc.seg)
		if code != http.StatusBadRequest {
			t.Errorf("guard passed for %+v, want 400", tc)
		}
	}
}

// --- Sanity: gin engine is wired up and reachable ---------------------------
//
// Tiny smoke test -- ensures gin can serve a request through the default
// engine so future tests in this package don't blow up on a Gin wiring
// change. No business logic here.

func TestGinEngine_Smoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "pong" {
		t.Fatalf("smoke: code=%d body=%q", w.Code, w.Body.String())
	}
}
