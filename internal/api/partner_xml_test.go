package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func writePartnerConfig(t *testing.T, baseURL, extraYAML string) {
	t.Helper()
	dir := t.TempDir()
	content := "active: default\npartners:\n  default:\n    api:\n      base_url: \"" + baseURL + "\"\n" + extraYAML
	path := filepath.Join(dir, "partner.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write partner.yaml: %v", err)
	}
	t.Setenv("PARTNER_CONFIG_PATH", path)
}

func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewPartnerXMLHandler(nil)
	r.GET("/common/channel-playouts-xml", h.GetChannelListXML)
	r.GET("/common/channel/epg/:channelId", h.GetEPGXML)
	return r
}

func TestChannelListHandlerReturnsXML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "ch-1", "name": "One", "img": "http://cdn/1.png"},
				{"id": "ch-2", "name": "Two"},
			},
		})
	}))
	defer upstream.Close()

	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel-playouts-xml", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("content type = %q", ct)
	}
	body := w.Body.String()
	if strings.Count(body, "<channel>") != 2 {
		t.Fatalf("expected 2 channels: %s", body)
	}
	if !strings.Contains(body, "<img></img>") {
		t.Fatalf("expected empty img element: %s", body)
	}
}

func TestChannelListHandlerEmpty(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer upstream.Close()

	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel-playouts-xml", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<channel-playouts></channel-playouts>") {
		t.Fatalf("expected empty root: %s", w.Body.String())
	}
}

func TestChannelListHandlerUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	writePartnerConfig(t, upstream.URL, "")
	router := newTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/common/channel-playouts-xml", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<error>") || !strings.Contains(body, "partner_unavailable") {
		t.Fatalf("expected XML error document: %s", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("error content type = %q", ct)
	}
}
