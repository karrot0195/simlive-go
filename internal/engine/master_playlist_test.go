package engine

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
)

// ctxWithTimeout is a small helper for redis ops in this file.
func ctxWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func redisReachableForEngine(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// --- TestBuildMasterPlaylist_Format -----------------------------------------

func TestBuildMasterPlaylist_Format(t *testing.T) {
	metas := []RenditionMeta{
		{Rendition: "360p", Width: 640, Height: 360, Bandwidth: 800_000},
	}
	uris := []string{"360p/playlist.m3u8"}
	out := buildMasterPlaylist(metas, uris)

	if !strings.HasPrefix(out, "#EXTM3U\n") {
		t.Fatalf("missing #EXTM3U header:\n%s", out)
	}
	if !strings.Contains(out, "#EXT-X-VERSION:3") {
		t.Fatalf("missing #EXT-X-VERSION:3:\n%s", out)
	}
	if !strings.Contains(out, "#EXT-X-INDEPENDENT-SEGMENTS") {
		t.Fatalf("missing #EXT-X-INDEPENDENT-SEGMENTS:\n%s", out)
	}
	if !strings.Contains(out, "BANDWIDTH=800000") {
		t.Fatalf("missing BANDWIDTH:\n%s", out)
	}
	if !strings.Contains(out, "RESOLUTION=640x360") {
		t.Fatalf("missing RESOLUTION:\n%s", out)
	}
	if !strings.Contains(out, "360p/playlist.m3u8\n") {
		t.Fatalf("missing media URI (M2 uses {rend}/playlist.m3u8):\n%s", out)
	}
}

// --- TestBuildMasterPlaylist_ThreeRungs (M1.5) -----------------------------

// TestBuildMasterPlaylist_ThreeRungs asserts the master advertises ALL three
// rungs (360p / 720p / 1080p) so a player can pick one and exercise
// variant switching. The 3 STREAM-INF lines are the headline M1.5
// deliverable: 1 shared segment cache + 3 advertised rungs.
func TestBuildMasterPlaylist_ThreeRungs(t *testing.T) {
	metas := []RenditionMeta{
		{Rendition: "360p", Width: 640, Height: 360, Bandwidth: 800_000},
		{Rendition: "720p", Width: 1280, Height: 720, Bandwidth: 2_500_000},
		{Rendition: "1080p", Width: 1920, Height: 1080, Bandwidth: 5_000_000},
	}
	uris := pickRenditionURIs(metas)
	out := buildMasterPlaylist(metas, uris)

	wantURIs := []string{"360p/playlist.m3u8\n", "720p/playlist.m3u8\n", "1080p/playlist.m3u8\n"}
	for _, want := range wantURIs {
		if !strings.Contains(out, want) {
			t.Fatalf("master missing rung URI %q in:\n%s", want, out)
		}
	}
	wantBWs := []string{"BANDWIDTH=800000", "BANDWIDTH=2500000", "BANDWIDTH=5000000"}
	for _, want := range wantBWs {
		if !strings.Contains(out, want) {
			t.Fatalf("master missing %q in:\n%s", want, out)
		}
	}
	wantRes := []string{"RESOLUTION=640x360", "RESOLUTION=1280x720", "RESOLUTION=1920x1080"}
	for _, want := range wantRes {
		if !strings.Contains(out, want) {
			t.Fatalf("master missing %q in:\n%s", want, out)
		}
	}
}

// --- TestBuildMasterPlaylist_SkipsIncompleteRungs ---------------------------

func TestBuildMasterPlaylist_SkipsIncompleteRungs(t *testing.T) {
	metas := []RenditionMeta{
		{Rendition: "360p", Width: 640, Height: 360, Bandwidth: 800_000}, // OK
		{Rendition: "720p", Width: 0, Height: 720, Bandwidth: 2_500_000}, // bad width
		{Rendition: "1080p", Width: 1920, Height: 1080, Bandwidth: 0},    // bad bandwidth
	}
	uris := pickRenditionURIs(metas)
	out := buildMasterPlaylist(metas, uris)

	if !strings.Contains(out, "360p/playlist.m3u8") {
		t.Fatalf("360p rung missing:\n%s", out)
	}
	if strings.Contains(out, "720p/playlist.m3u8") {
		t.Fatalf("720p rung should be skipped (width=0):\n%s", out)
	}
	if strings.Contains(out, "1080p/playlist.m3u8") {
		t.Fatalf("1080p rung should be skipped (bandwidth=0):\n%s", out)
	}
}

// --- TestBuildMasterPlaylist_RefsRelativePath ------------------------------

func TestBuildMasterPlaylist_RefsRelativePath(t *testing.T) {
	metas := []RenditionMeta{
		{Rendition: "360p", Width: 640, Height: 360, Bandwidth: 1},
		{Rendition: "720p", Width: 1280, Height: 720, Bandwidth: 2},
	}
	uris := pickRenditionURIs(metas)
	out := buildMasterPlaylist(metas, uris)
	for _, want := range []string{"360p/playlist.m3u8\n", "720p/playlist.m3u8\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing relative URI %q in:\n%s", want, out)
		}
		// Must NOT be an absolute URL (would break relative-to-master semantics).
		if strings.Contains(out, "http://"+want) || strings.Contains(out, "https://"+want) {
			t.Fatalf("URI must be relative, not absolute:\n%s", out)
		}
	}
}

// --- TestSortRungsByLadder --------------------------------------------------

func TestSortRungsByLadder(t *testing.T) {
	in := []RenditionMeta{
		{Rendition: "1080p"},
		{Rendition: "360p"},
		{Rendition: "720p"},
		{Rendition: "weird"},
	}
	out := sortRungsByLadder(in)
	want := []string{"360p", "720p", "1080p", "weird"}
	for i, w := range want {
		if out[i].Rendition != w {
			t.Fatalf("sorted[%d] = %q, want %q", i, out[i].Rendition, w)
		}
	}
}

// --- TestPickRenditionURIs + TestRungDir / TestChannelDir helpers ------------

func TestPickRenditionURIs(t *testing.T) {
	metas := []RenditionMeta{{Rendition: "360p"}, {Rendition: ""}}
	got := pickRenditionURIs(metas)
	// M3: per-rendition media playlist URIs are "playlist_{rend}.m3u8" in the
	// channel root. Empty rendition rescues to DefaultRendition.
	want := []string{"360p/playlist.m3u8", "360p/playlist.m3u8"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pickRenditionURIs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestChannelDirAndRedisKeys(t *testing.T) {
	// M3: channelDir is the channel root; no per-rung subdir.
	if got := channelDir("/tmp/out", "ch1"); got != "/tmp/out/ch1" {
		t.Fatalf("channelDir = %q", got)
	}
	// Per-rendition Redis key shape "simlive:hls:{ch}:rendition:{rend}".
	if got := renditionMediaRedisKey("ch1", "360p"); got != "simlive:hls:ch1:rendition:360p" {
		t.Fatalf("renditionMediaRedisKey = %q", got)
	}
	if got := masterRedisKey("ch1"); got != "simlive:hls:ch1" {
		t.Fatalf("masterRedisKey = %q", got)
	}
	if got := isValidRenditionLabel("720p"); !got {
		t.Fatalf("isValidRenditionLabel(720p) = false, want true")
	}
	if got := isValidRenditionLabel("4k"); got {
		t.Fatalf("isValidRenditionLabel(4k) = true, want false")
	}
}

func TestActiveLadder_HasThreeRungs(t *testing.T) {
	// M1.5: ActiveLadder is the cold-start fallback when no per-channel
	// metadata is in Redis yet. It MUST list all 3 rungs so the master
	// always advertises the full ladder.
	if len(ActiveLadder) != 3 {
		t.Fatalf("len(ActiveLadder) = %d, want 3", len(ActiveLadder))
	}
	wantLabels := []string{"360p", "720p", "1080p"}
	for i, want := range wantLabels {
		if ActiveLadder[i].Rendition != want {
			t.Fatalf("ActiveLadder[%d].Rendition = %q, want %q", i, ActiveLadder[i].Rendition, want)
		}
	}
}

// --- TestSyncLocalCacheDir_PerRungDir ---------------------------------------
//
// M2: each rung has its OWN seg_N.ts set in its own subdir. syncLocalCacheDir
// is called on a rung subdir and GCs stale .ts/.pts files there. The master
// playlist (channel root) and other rung subdirs are untouched.
func TestSyncLocalCacheDir_PerRungDir(t *testing.T) {
	tmp := t.TempDir()
	// M3.2: layout is {TMP_DIR}/{ch}/{rend}/. syncLocalCacheDir is called on
	// a rung subdir; it GCs stale .ts/.pts files in THAT subdir. The master
	// (channel root) and other rung subdirs are untouched.
	rDir := filepath.Join(tmp, "ch1", "360p")
	if err := os.MkdirAll(rDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Active window: only seg_1.ts. Other .ts files + their markers should
	// be removed. The rung's media playlist must be preserved (not .ts).
	if err := os.WriteFile(filepath.Join(rDir, "seg_1.ts"), []byte("a"), 0644); err != nil {
		t.Fatalf("write seg_1.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rDir, "seg_2.ts"), []byte("b"), 0644); err != nil {
		t.Fatalf("write seg_2.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rDir, "seg_2.ts.pts"), []byte(""), 0644); err != nil {
		t.Fatalf("write seg_2.ts.pts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rDir, "playlist.m3u8"), []byte("rung360p"), 0644); err != nil {
		t.Fatalf("write playlist.m3u8: %v", err)
	}

	activeMap := map[string]bool{"seg_1.ts": true}
	syncLocalCacheDir(rDir, activeMap)

	// seg_1.ts must remain; seg_2.ts + its marker must be gone.
	if _, err := os.Stat(filepath.Join(rDir, "seg_1.ts")); err != nil {
		t.Fatalf("seg_1.ts was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rDir, "seg_2.ts")); !os.IsNotExist(err) {
		t.Fatalf("seg_2.ts should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(rDir, "seg_2.ts.pts")); !os.IsNotExist(err) {
		t.Fatalf("seg_2.ts.pts should be removed, stat err = %v", err)
	}
	// The rung's media playlist must be preserved (not .ts / .ts.pts).
	if _, err := os.Stat(filepath.Join(rDir, "playlist.m3u8")); err != nil {
		t.Fatalf("playlist.m3u8 was removed by syncLocalCacheDir: %v", err)
	}
}

// --- TestGenerateMasterPlaylist_ThreeRungs (M1.5 end-to-end) ---------------
//
// Drives a real Engine through a tick-equivalent call sequence (no
// per-rung candidates) and verifies the on-disk master lists all 3
// rungs with the right STREAM-INF lines. M1.5 is the headline: the
// master advertises the full ladder, not just the active rung.

func TestGenerateMasterPlaylist_ThreeRungs(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForEngine(addr) {
		t.Skipf("redis not reachable at %s; skipping", addr)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := "test-master-three-rungs"
	t.Cleanup(func() {
		ctx2, cancel2 := ctxWithTimeout()
		defer cancel2()
		rdb.Del(ctx2, masterRedisKey(chID))
	})

	tmpDir := t.TempDir()
	t.Setenv("TMP_DIR", tmpDir)
	e := NewEngine(rdb, nil, 1)

	// Use the hard-coded ActiveLadder as the input (cold-start path).
	meta := ActiveLadder
	e.generateMasterPlaylist(ctx, chID, meta)

	// File written with all 3 rung STREAM-INF lines.
	playlistPath := filepath.Join(tmpDir, chID, "playlist.m3u8")
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatalf("read master playlist: %v", err)
	}
	out := string(data)
	for _, want := range []string{
		"BANDWIDTH=800000,RESOLUTION=640x360",
		"BANDWIDTH=2500000,RESOLUTION=1280x720",
		"BANDWIDTH=5000000,RESOLUTION=1920x1080",
		"360p/playlist.m3u8\n",
		"720p/playlist.m3u8\n",
		"1080p/playlist.m3u8\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("master missing %q in:\n%s", want, out)
		}
	}

	// Redis master key written.
	rawRedis, err := rdb.Get(ctx, masterRedisKey(chID)).Result()
	if err != nil {
		t.Fatalf("GET master: %v", err)
	}
	for _, want := range []string{"BANDWIDTH=800000", "BANDWIDTH=2500000", "BANDWIDTH=5000000"} {
		if !strings.Contains(rawRedis, want) {
			t.Fatalf("redis master missing %q in:\n%s", want, rawRedis)
		}
	}
}

// --- TestGenerateMasterPlaylist_ColdStartEmptyMetadata ----------------------
//
// When the caller passes an empty meta slice, generateMasterPlaylist must
// still write a master file (header + empty body) so a player reload
// returns 200 rather than 404. ActiveLadder fallback is handled by the
// caller (activeRungMeta), not by generateMasterPlaylist itself, so
// passing nil here is a defensive sanity check.

func TestGenerateMasterPlaylist_ColdStartEmptyMetadata(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForEngine(addr) {
		t.Skipf("redis not reachable at %s; skipping", addr)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := "test-master-cold-no-meta"
	t.Cleanup(func() {
		ctx2, cancel2 := ctxWithTimeout()
		defer cancel2()
		rdb.Del(ctx2, masterRedisKey(chID))
	})

	tmpDir := t.TempDir()
	t.Setenv("TMP_DIR", tmpDir)
	e := NewEngine(rdb, nil, 1)

	e.generateMasterPlaylist(ctx, chID, nil)

	playlistPath := filepath.Join(tmpDir, chID, "playlist.m3u8")
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatalf("read master playlist (cold): %v", err)
	}
	if !strings.HasPrefix(string(data), "#EXTM3U") {
		t.Fatalf("cold-start master must still begin with #EXTM3U, got: %s", data)
	}
}

// --- TestActiveRungMeta_FallsBackToActiveLadder ---------------------------
//
// Verifies that activeRungMeta returns the hard-coded ActiveLadder when
// no per-channel metadata is in Redis. This is the cold-start behavior
// that lets the master list 3 rungs before the planner has run.

func TestActiveRungMeta_FallsBackToActiveLadder(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForEngine(addr) {
		t.Skipf("redis not reachable at %s; skipping", addr)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := "test-active-rung-meta-cold"
	metaKey := renditionMetadataRedisKey(chID)
	t.Cleanup(func() {
		ctx2, cancel2 := ctxWithTimeout()
		defer cancel2()
		rdb.Del(ctx2, metaKey)
	})
	if err := rdb.Del(ctx, metaKey).Err(); err != nil {
		t.Fatalf("DEL pre-state: %v", err)
	}

	e := NewEngine(rdb, nil, 1)
	got := e.activeRungMeta(ctx, chID)
	if len(got) != len(ActiveLadder) {
		t.Fatalf("activeRungMeta = %d rungs, want %d (fallback)", len(got), len(ActiveLadder))
	}
	for i, want := range ActiveLadder {
		if got[i].Rendition != want.Rendition {
			t.Fatalf("activeRungMeta[%d] = %q, want %q", i, got[i].Rendition, want.Rendition)
		}
	}
}

// --- helpers ---------------------------------------------------------------

func keysOf(m map[string][]models.SegmentItem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func seqsOf(segs []models.SegmentItem) []int64 {
	out := make([]int64, len(segs))
	for i, s := range segs {
		out[i] = s.Sequence
	}
	return out
}
