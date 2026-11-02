package partner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testProfile(baseURL string) *PartnerProfile {
	return &PartnerProfile{
		API: PartnerAPI{
			BaseURL: baseURL,
			Timeout: "5s",
			Headers: map[string]string{"X-Partner": "alpha"},
			Endpoints: PartnerEndpoints{
				ChannelList:   "/cli",
				ChannelDetail: "/cli/{channel_id}",
				Schedule:      "/epg/{epg_id}/schedules",
			},
			ChannelListQuery: map[string]string{"is_playout_assigned": "true"},
			ScheduleQuery:    map[string]string{"$skip": "0"},
		},
	}
}

func TestClientFetchChannelList(t *testing.T) {
	var gotHeader, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Partner")
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "ch-1", "name": "One"}},
		})
	}))
	defer srv.Close()

	c := NewClient(testProfile(srv.URL))
	items, err := c.FetchChannelList(context.Background())
	if err != nil {
		t.Fatalf("FetchChannelList: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "ch-1" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if gotHeader != "alpha" {
		t.Fatalf("configurable header not sent: %q", gotHeader)
	}
	if gotQuery != "is_playout_assigned=true" {
		t.Fatalf("query merge = %q", gotQuery)
	}
}

func TestClientFetchSchedulePlaceholders(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer srv.Close()

	c := NewClient(testProfile(srv.URL))
	if _, err := c.FetchSchedule(context.Background(), "epg-9", "2026-11-02", "+07:00"); err != nil {
		t.Fatalf("FetchSchedule: %v", err)
	}
	if gotPath != "/epg/epg-9/schedules" {
		t.Fatalf("epg_id placeholder not substituted: %q", gotPath)
	}
	q := gotQuery
	for _, want := range []string{"%24date=2026-11-02", "tz=%2B07%3A00", "%24skip=0"} {
		if !contains(q, want) {
			t.Fatalf("schedule query missing %q in %q", want, q)
		}
	}
}

func TestClientFetchChannelDetailPlaceholder(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "ch-1",
			"channelEpg": map[string]any{"id": "epg-1"},
		})
	}))
	defer srv.Close()

	c := NewClient(testProfile(srv.URL))
	id, err := c.FetchChannelDetail(context.Background(), "ch-1")
	if err != nil {
		t.Fatalf("FetchChannelDetail: %v", err)
	}
	if id != "epg-1" {
		t.Fatalf("epg id = %q", id)
	}
	if gotPath != "/cli/ch-1" {
		t.Fatalf("channel_id placeholder not substituted: %q", gotPath)
	}
}

func TestClientFetchChannelDetailNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(testProfile(srv.URL))
	if _, err := c.FetchChannelDetail(context.Background(), "missing"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
