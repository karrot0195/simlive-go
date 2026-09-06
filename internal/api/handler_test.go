package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
)

// These tests exercise the path-guard logic for the M2 routes in
// cmd/api/main.go by recreating the guards inline. The intent is to lock
// down the rejection behavior WITHOUT spinning up Redis / a full cmd/api
// integration test. A separate cmd/api/main_test.go could exercise the
// full router end-to-end; for now the guards are validated here.
//
// M2 route shape (matching cmd/api/main.go):
//   GET /live/:channel_id/playlist.m3u8         (master)
//   GET /live/:channel_id/:rendition/playlist.m3u8   (per-rung media playlist)
//   GET /live/:channel_id/:rendition/:segment   (per-rung .ts segments)

// rungPlaylistGuard reproduces the per-param guards for the
// `GET /live/:channel_id/:rendition/playlist.m3u8` route in cmd/api/main.go.
// `rend` is the value Gin extracts from the URL (e.g. "360p"). Returns the
// (statusCode, body) tuple the live handler would emit.
func rungPlaylistGuard(channelID, rend string) (int, string) {
	if strings.Contains(channelID, "..") || strings.Contains(channelID, "/") ||
		strings.Contains(rend, "..") || strings.Contains(rend, "/") {
		return http.StatusBadRequest, "Invalid path"
	}
	switch rend {
	case "360p", "720p", "1080p":
		// valid ladder rung
	default:
		return http.StatusNotFound, "Unknown rendition"
	}
	return http.StatusOK, "passed-guard"
}

// segmentGuard reproduces the per-param guards for the
// `GET /live/:channel_id/:rendition/:segment` route (per-rung segments).
func segmentGuard(channelID, rend, segment string) (int, string) {
	if strings.Contains(channelID, "..") || strings.Contains(channelID, "/") ||
		strings.Contains(rend, "..") || strings.Contains(rend, "/") ||
		strings.Contains(segment, "..") || strings.Contains(segment, "/") ||
		strings.Contains(segment, `\`) {
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
// "4k" or "random") is a 404 -- not a 200 with the master. This guards
// against typo'd client URLs silently falling through to the master playlist.

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
	for _, rend := range []string{"360p", "720p", "1080p"} {
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
	code, _ := segmentGuard("ch1", "360p", "seg_1.exe")
	if code != http.StatusNotFound {
		t.Fatalf("non-.ts segment: code = %d, want 404", code)
	}
}

// --- TestSegmentGuard_AcceptsValid -----------------------------------------

func TestSegmentGuard_AcceptsValid(t *testing.T) {
	code, body := segmentGuard("ch1", "360p", "seg_42.ts")
	if code != http.StatusOK {
		t.Fatalf("valid segment: code = %d, want 200 (passes guard); body = %q", code, body)
	}
}

// --- TestSegmentGuard_RejectsDotDotAndSlash --------------------------------

func TestSegmentGuard_RejectsDotDotAndSlash(t *testing.T) {
	cases := []struct{ ch, rend, seg string }{
		{"ch1", "360p", "../etc/passwd"},
		{"..", "360p", "seg_1.ts"},
		{"ch1", "360p", "sub/seg_1.ts"},
		{"ch1", "360p", "sub\\seg_1.ts"},
		{"ch1", "360p/..", "seg_1.ts"},
	}
	for _, tc := range cases {
		code, _ := segmentGuard(tc.ch, tc.rend, tc.seg)
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

// =========================================================================
//   Tests for the /api/channels/* CRUD + start/stop surface added in
//   `docs/300826-create-rest-api-for-playout-channels-management.md`.
//   These tests are pure (no Postgres, no Redis) — they exercise the
//   pure helpers (parsePagination, updateColumnsAndValues,
//   assignmentMap, channelIDGuard) and the request/response model
//   shape. The integration smoke test (DB-seeded curl matrix) lives in
//   the plan doc §6.
// =========================================================================

// helper: build a gin context + recorder with the given query string.
func newCtxForQuery(rawQuery string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	if rawQuery == "" {
		req = httptest.NewRequest(http.MethodGet, "/", nil)
	}
	// Parse the query into req.URL so c.Query() works.
	if rawQuery != "" {
		v, _ := url.ParseQuery(rawQuery)
		req.URL.RawQuery = v.Encode()
	}
	c.Request = req
	return c, w
}

// --- parsePagination ----------------------------------------------------

func TestParsePagination_Defaults(t *testing.T) {
	c, _ := newCtxForQuery("")
	page, size := parsePagination(c)
	if page != 1 || size != defaultListPageSize {
		t.Errorf("defaults: page=%d size=%d, want 1/%d", page, size, defaultListPageSize)
	}
}

func TestParsePagination_ClampsPageSize(t *testing.T) {
	c, _ := newCtxForQuery("page_size=999")
	_, size := parsePagination(c)
	if size != maxListPageSize {
		t.Errorf("clamp high: size=%d, want %d", size, maxListPageSize)
	}
	c, _ = newCtxForQuery("page_size=0")
	_, size = parsePagination(c)
	if size != defaultListPageSize {
		t.Errorf("clamp zero: size=%d, want %d", size, defaultListPageSize)
	}
	c, _ = newCtxForQuery("page_size=-5")
	_, size = parsePagination(c)
	if size != defaultListPageSize {
		t.Errorf("clamp negative: size=%d, want %d", size, defaultListPageSize)
	}
}

func TestParsePagination_AcceptsValid(t *testing.T) {
	c, _ := newCtxForQuery("page=3&page_size=50")
	page, size := parsePagination(c)
	if page != 3 || size != 50 {
		t.Errorf("valid: page=%d size=%d, want 3/50", page, size)
	}
}

// --- updateColumnsAndValues + assignmentMap -----------------------------

func ptrStr(s string) *string { return &s }
func ptrBool(b bool) *bool    { return &b }

func TestUpdateColumnsAndValues_SkipsZeroFields(t *testing.T) {
	req := &models.UpdateChannelReq{Status: ptrStr("PREPARE")}
	cols, vals := updateColumnsAndValues(req)
	if len(cols) != 1 || cols[0] != "status" || vals[0] != "PREPARE" {
		t.Errorf("single status: cols=%v vals=%v, want [status] [PREPARE]", cols, vals)
	}
}

func TestUpdateColumnsAndValues_AllFields(t *testing.T) {
	req := &models.UpdateChannelReq{
		Name:            ptrStr("New"),
		BaseName:        ptrStr("new-base"),
		ContentTimezone: ptrStr("Asia/Tokyo"),
		ChannelEpgID:    ptrStr("epg-2"),
		IsActive:        ptrBool(false),
		Status:          ptrStr("INACTIVE"),
	}
	cols, vals := updateColumnsAndValues(req)
	if len(cols) != 6 {
		t.Errorf("all fields: got %d cols (%v), want 6", len(cols), cols)
	}
	m := assignmentMap(cols, vals)
	want := map[string]interface{}{
		"name":             "New",
		"base_name":        "new-base",
		"content_timezone": "Asia/Tokyo",
		"channel_epg_id":   "epg-2",
		"is_active":        false,
		"status":           "INACTIVE",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("map[%q] = %v, want %v", k, m[k], v)
		}
	}
}

func TestUpdateColumnsAndValues_Empty(t *testing.T) {
	req := &models.UpdateChannelReq{}
	cols, vals := updateColumnsAndValues(req)
	if len(cols) != 0 || len(vals) != 0 {
		t.Errorf("empty: cols=%v vals=%v, want both empty", cols, vals)
	}
}

// --- channelIDGuard -----------------------------------------------------

func TestChannelIDGuard_AcceptsValid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "ch-abc_123"}}
	if !channelIDGuard(c, "ch-abc_123") {
		t.Errorf("valid id rejected")
	}
}

func TestChannelIDGuard_RejectsDotDot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if channelIDGuard(c, "../etc") {
		t.Errorf("dotdot accepted")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("dotdot code=%d, want 400", w.Code)
	}
}

func TestChannelIDGuard_RejectsSlash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if channelIDGuard(c, "ch/abc") {
		t.Errorf("slash accepted")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("slash code=%d, want 400", w.Code)
	}
}

// --- Breaker key prefix contract ----------------------------------------
//
// The planner and the api handler both pin the same string. If a future
// refactor renames one but not the other, the test catches it.

func TestBreakerKeyPrefix_MatchesPlanner(t *testing.T) {
	if breakerKeyPrefix != "planner:failures:" {
		t.Errorf("handler breakerKeyPrefix = %q, want %q", breakerKeyPrefix, "planner:failures:")
	}
	want := breakerKeyPrefix + "ch-abc"
	if want != "planner:failures:ch-abc" {
		t.Errorf("format: %q, want %q", want, "planner:failures:ch-abc")
	}
}

// --- Status enum validation --------------------------------------------

func TestAllowedStatuses(t *testing.T) {
	for _, ok := range []string{"PREPARE", "LIVE", "INACTIVE"} {
		if !allowedStatuses[ok] {
			t.Errorf("%q should be allowed", ok)
		}
	}
	for _, bad := range []string{"FOO", "active", "ERROR", "ACTIVE"} {
		if allowedStatuses[bad] {
			t.Errorf("%q should be rejected (ERROR is planner-only, ACTIVE was removed from the lifecycle)", bad)
		}
	}
}

// --- UpdateChannelReq round-trip ---------------------------------------
//
// Pointer fields must round-trip through JSON correctly. Ensures
// the JSON tags are not accidentally changed to non-pointer fields.

func TestUpdateChannelReq_RoundTrip(t *testing.T) {
	original := models.UpdateChannelReq{
		Name:     ptrStr("Renamed"),
		IsActive: ptrBool(true),
	}
	// Unmarshal a partial body — only `name` and `is_active` set.
	body := `{"name":"Renamed","is_active":true}`
	var got models.UpdateChannelReq
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name == nil || *got.Name != *original.Name {
		t.Errorf("name: got %v, want %q", got.Name, *original.Name)
	}
	if got.IsActive == nil || *got.IsActive != *original.IsActive {
		t.Errorf("is_active: got %v, want %v", got.IsActive, *original.IsActive)
	}
	// Unset fields stay nil after a round-trip on a partial body.
	if got.Status != nil {
		t.Errorf("status: got %v, want nil", got.Status)
	}
	if got.BaseName != nil {
		t.Errorf("base_name: got %v, want nil", got.BaseName)
	}
}

// =========================================================================
//   Tests for purgeChannelRedisData (Stop / Delete behavior).
//
// These tests are REDIS-gated: they require a live Redis at REDIS_ADDR
// (default localhost:6371, see internal/utils/envs.go). If no Redis is
// reachable the tests SKIP themselves, mirroring the pattern in
// internal/planner/horizon_rollover_test.go.
//
// The test seeds each of the 6 key families for a synthetic channel
// id, calls purgeChannelRedisData, and asserts:
//
//   1. The 5 purged keys (hls + timeline + renditions + cursor) are
//      absent afterwards.
//   2. The breaker key (planner:failures:{ch}) is INTENTIONALLY
//      PRESERVED — this is the documented contract with StartChannel.
//   3. An unrelated channel's data is not touched (the purge is
//      scoped to the given id, not a wildcard).
//   4. A second purge (the idempotent stop path) is a no-op.
//   5. A nil redis client is a safe no-op (no panic).
// =========================================================================

func TestPurgeChannelRedisData_RemovesAllKeyFamiliesExceptBreaker(t *testing.T) {
	rdb := purgeTestRedis(t)
	h := &ChannelHandler{rdb: rdb}
	id := "purge-test-ch-1"

	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()

	// Seed every key family the planner + engine write for this id.
	seed := map[string]string{
		hlsRedisKeyPrefix + id:                      "MASTER",
		hlsRedisKeyPrefix + id + ":rendition:360p":  "360P",
		hlsRedisKeyPrefix + id + ":rendition:720p":  "720P",
		hlsRedisKeyPrefix + id + ":rendition:1080p": "1080P",
		timelineRedisKeyPrefix + id:                 `{"sequence":1}`,
		timelineRedisKeyPrefix + id + ":360p":       `{"sequence":1,"rendition":"360p"}`,
		timelineRedisKeyPrefix + id + ":720p":       `{"sequence":1,"rendition":"720p"}`,
		timelineRedisKeyPrefix + id + ":1080p":      `{"sequence":1,"rendition":"1080p"}`,
		renditionsRedisKeyPrefix + id:               `[{"rendition":"360p"}]`,
		cursorRedisKeyPrefix + id:                   "1700000000",
		breakerKeyPrefix + id:                       "3",
	}
	for k, v := range seed {
		if err := rdb.Set(ctx, k, v, 0).Err(); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}

	h.purgeChannelRedisData(id)

	// 1. The purged keys are gone (hls + per-rung hls + per-rung timeline
	// + flat timeline + renditions + cursor).
	for _, k := range []string{
		hlsRedisKeyPrefix + id,
		hlsRedisKeyPrefix + id + ":rendition:360p",
		hlsRedisKeyPrefix + id + ":rendition:720p",
		hlsRedisKeyPrefix + id + ":rendition:1080p",
		timelineRedisKeyPrefix + id,
		timelineRedisKeyPrefix + id + ":360p",
		timelineRedisKeyPrefix + id + ":720p",
		timelineRedisKeyPrefix + id + ":1080p",
		renditionsRedisKeyPrefix + id,
		cursorRedisKeyPrefix + id,
	} {
		exists, err := rdb.Exists(ctx, k).Result()
		if err != nil {
			t.Fatalf("EXISTS %s: %v", k, err)
		}
		if exists != 0 {
			t.Errorf("key %q still present after purge (exists=%d), want 0", k, exists)
		}
	}

	// 2. The breaker is preserved across the purge.
	got, err := rdb.Get(ctx, breakerKeyPrefix+id).Result()
	if err != nil {
		t.Fatalf("GET breaker: %v", err)
	}
	if got != "3" {
		t.Errorf("breaker = %q, want %q (must be preserved across stop)", got, "3")
	}
}

func TestPurgeChannelRedisData_DoesNotTouchOtherChannels(t *testing.T) {
	rdb := purgeTestRedis(t)
	h := &ChannelHandler{rdb: rdb}
	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()

	for _, id := range []string{"keep-ch", "drop-ch"} {
		rdb.Set(ctx, hlsRedisKeyPrefix+id, "M", 0)
		rdb.Set(ctx, timelineRedisKeyPrefix+id, "T", 0)
	}

	h.purgeChannelRedisData("drop-ch")

	if e, _ := rdb.Exists(ctx, hlsRedisKeyPrefix+"drop-ch").Result(); e != 0 {
		t.Error("drop-ch hls key still present")
	}
	if e, _ := rdb.Exists(ctx, timelineRedisKeyPrefix+"drop-ch").Result(); e != 0 {
		t.Error("drop-ch timeline still present")
	}
	if e, _ := rdb.Exists(ctx, hlsRedisKeyPrefix+"keep-ch").Result(); e != 1 {
		t.Error("keep-ch hls key was incorrectly purged")
	}
	if e, _ := rdb.Exists(ctx, timelineRedisKeyPrefix+"keep-ch").Result(); e != 1 {
		t.Error("keep-ch timeline was incorrectly purged")
	}
}

func TestPurgeChannelRedisData_Idempotent(t *testing.T) {
	rdb := purgeTestRedis(t)
	h := &ChannelHandler{rdb: rdb}
	id := "idem-ch"

	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()
	rdb.Set(ctx, hlsRedisKeyPrefix+id, "M", 0)

	h.purgeChannelRedisData(id)
	h.purgeChannelRedisData(id) // DEL of absent keys must not error

	if e, _ := rdb.Exists(ctx, hlsRedisKeyPrefix+id).Result(); e != 0 {
		t.Error("hls key still present after double purge")
	}
}

func TestPurgeChannelRedisData_NilRedisIsNoOp(t *testing.T) {
	h := &ChannelHandler{rdb: nil}
	// Must not panic.
	h.purgeChannelRedisData("any-id")
}

// --- shared Redis test fixture ------------------------------------------

func purgeTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := redisAddr()
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	ctx, cancel := contextWithTimeout(2 * time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Skipf("no Redis at %s: %v", addr, err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Fatalf("FlushDB: %v", err)
	}
	return rdb
}

func redisAddr() string {
	if v := envGet("REDIS_ADDR"); v != "" {
		return v
	}
	return "localhost:6371"
}

// contextWithTimeout is a thin helper that mirrors the production
// handler's `context.WithTimeout(context.Background(), d)` pattern.
// Using context.Background() (not the gin request context) ensures
// the Redis call survives a client-side cancellation, which is the
// same contract production code honours.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// envGet is a tiny wrapper around os.Getenv so the test file's
// import block stays small.
func envGet(k string) string { return os.Getenv(k) }
