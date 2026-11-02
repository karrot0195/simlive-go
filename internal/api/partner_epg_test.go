package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// epgUpstream returns a stub partner API that serves channel detail and schedule.
func epgUpstream(t *testing.T, knownChannel bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/channel-epg/") && strings.HasSuffix(r.URL.Path, "/schedules"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"local_date_start": "2026-11-02 00:00:00.000",
						"local_date_end":   "2026-11-02 01:00:00.000",
						"programme": map[string]any{
							"name": "Morning News",
							"programmeStreams": []any{
								map[string]any{"stream_id": "s1", "is_vod": true, "is_trailer": false},
							},
						},
					},
				},
			})
		default: // channel detail
			if !knownChannel {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "ch-1",
				"channelEpg": map[string]any{"id": "epg-1"},
			})
		}
	}))
}

func grant(t *testing.T) *httptest.Server { return epgUpstream(t, true) }
func deny(t *testing.T) *httptest.Server  { return epgUpstream(t, false) }

func TestEPGHandlerReturnsGuide(t *testing.T) {
	upstream := grant(t)
	defer upstream.Close()
	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel/epg/ch-1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"<epg>", "<channelId>ch-1</channelId>", "<start>", "<stop>", "<streamId>s1</streamId>", "<ads>false</ads>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestEPGHandlerUnknownChannel404(t *testing.T) {
	upstream := deny(t)
	defer upstream.Close()
	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel/epg/ghost", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "channel_not_found") {
		t.Fatalf("expected channel_not_found: %s", w.Body.String())
	}
}

func TestEPGHandlerPathGuard(t *testing.T) {
	upstream := grant(t)
	defer upstream.Close()
	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	// Traversal characters must be rejected before any lookup. A plain `..`
	// reaches the handler and yields 400; encoded slashes are rejected by the
	// router (404). Either way the request never reaches the partner API.
	for _, path := range []string{
		"/common/channel/epg/..",
		"/common/channel/epg/..%2Fetc",
		"/common/channel/epg/a%2Fb",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
			t.Fatalf("path %q: status = %d, want 400 or 404", path, w.Code)
		}
	}

	// The handler guard itself rejects `..` with the documented error code.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel/epg/..", nil)
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "invalid_channel_id") {
		t.Fatalf("expected invalid_channel_id: %s", w.Body.String())
	}
}

func TestEPGHandlerInvalidDate(t *testing.T) {
	upstream := grant(t)
	defer upstream.Close()
	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel/epg/ch-1?date=not-a-date", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_date") {
		t.Fatalf("expected invalid_date: %s", w.Body.String())
	}
}

func TestEPGHandlerMetadataGating(t *testing.T) {
	upstream := grant(t)
	defer upstream.Close()
	writePartnerConfig(t, upstream.URL, "    xml:\n      epg:\n        custom_fields:\n          - name: \"title\"\n            source: \"programme.name\"\n            when: \"metadata\"\n")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel/epg/ch-1", nil)
	router.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "<title>") {
		t.Fatalf("metadata should be absent without flag: %s", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/common/channel/epg/ch-1?$include_metadata=true", nil)
	router.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), "<title>Morning News</title>") {
		t.Fatalf("metadata should appear with flag: %s", w2.Body.String())
	}
}

func TestEPGHandlerEmptyGuide(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/channel-epg/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ch-1", "channelEpg": map[string]any{"id": "epg-1"}})
	}))
	defer upstream.Close()
	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel/epg/ch-1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<epg>") || strings.Contains(body, "<epgitem>") {
		t.Fatalf("expected empty guide: %s", body)
	}
}
