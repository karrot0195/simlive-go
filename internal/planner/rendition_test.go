package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Each test that exercises activeRenditions() uses t.Setenv to override
// SIMLIVE_QUALITY / SIMLIVE_RENDITIONS, and a sync.Once reset so the
// process-level cache (activeRenditionsOnce) doesn't leak across tests.
//
// We can't easily *reset* a sync.Once after Do, so each subtest that mutates
// these env vars must call resetActiveRenditionsCache() at the top.
// resetActiveRenditionsCache swaps the sync.Once with a fresh one and
// zeroes the slice. NOT thread-safe -- only call from test goroutines.

func resetActiveRenditionsCache() {
	activeRenditionsOnce = sync.Once{}
	activeRenditionsList = nil
}

// redisReachableForRendition does a quick TCP dial so the test can skip
// cleanly when no Redis is running locally.
func redisReachableForRendition(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ctxForRendition returns a 5s-timeout context for redis ops in this file.
func ctxForRendition() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// --- TestActiveRenditions_DefaultsToFullLadder -------------------------------

func TestActiveRenditions_DefaultsToFullLadder(t *testing.T) {
	resetActiveRenditionsCache()
	t.Setenv("SIMLIVE_RENDITIONS", "")
	t.Setenv("SIMLIVE_QUALITY", "720p")

	p := &Planner{}
	got := p.activeRenditions()
	// M2: unset SIMLIVE_RENDITIONS -> full ladder, regardless of SIMLIVE_QUALITY.
	want := []string{"360p", "720p", "1080p"}
	if len(got) != len(want) {
		t.Fatalf("activeRenditions() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("activeRenditions() = %v, want %v", got, want)
		}
	}
}

// --- TestActiveRenditions_ParsesCommaList ------------------------------------

func TestActiveRenditions_ParsesCommaList(t *testing.T) {
	resetActiveRenditionsCache()
	t.Setenv("SIMLIVE_RENDITIONS", " 720p ")
	t.Setenv("SIMLIVE_QUALITY", "")

	p := &Planner{}
	got := p.activeRenditions()
	if len(got) != 1 || got[0] != "720p" {
		t.Fatalf("activeRenditions() trimmed = %v, want [720p]", got)
	}

	resetActiveRenditionsCache()
	t.Setenv("SIMLIVE_RENDITIONS", ",360p,,720p,,")
	got = p.activeRenditions()
	// M2: all listed rungs are returned in order; empty entries dropped.
	want := []string{"360p", "720p"}
	if len(got) != len(want) {
		t.Fatalf("activeRenditions() cleaned = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("activeRenditions() cleaned = %v, want %v", got, want)
		}
	}
}

// --- TestActiveRenditions_ReturnsAllListedRungs -------------------------------

func TestActiveRenditions_ReturnsAllListedRungs(t *testing.T) {
	resetActiveRenditionsCache()
	t.Setenv("SIMLIVE_RENDITIONS", "360p,720p,1080p")
	t.Setenv("SIMLIVE_QUALITY", "")

	p := &Planner{}
	got := p.activeRenditions()
	want := []string{"360p", "720p", "1080p"}
	if len(got) != len(want) {
		t.Fatalf("activeRenditions() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("activeRenditions() = %v, want %v", got, want)
		}
	}
}

// --- TestProbeBitrateFallback_Formula ----------------------------------------

func TestProbeBitrateFallback_Formula(t *testing.T) {
	// In-range formula: 640*360*0.08 = 18432 -> clamped to lo=100_000.
	if got := probeBitrateFallback(640, 360); got != 100_000 {
		t.Fatalf("probeBitrateFallback(640,360) = %d, want 100000 (clamp)", got)
	}
	// 1920*1080*0.08 = 165888 -> in range, no clamp.
	if got := probeBitrateFallback(1920, 1080); got != 165888 {
		t.Fatalf("probeBitrateFallback(1920,1080) = %d, want 165888", got)
	}
	// 7680*4320*0.08 = 2654208 -> above hi=20_000_000, must clamp.
	if got := probeBitrateFallback(7680, 4320); got != 20_000_000 {
		t.Fatalf("probeBitrateFallback(7680,4320) = %d, want 20000000 (clamp)", got)
	}
	// Zero / negative dims return lo (defensive).
	if got := probeBitrateFallback(0, 0); got != 100_000 {
		t.Fatalf("probeBitrateFallback(0,0) = %d, want 100000", got)
	}
	if got := probeBitrateFallback(-1, 720); got != 100_000 {
		t.Fatalf("probeBitrateFallback(-1,720) = %d, want 100000", got)
	}
}

// --- TestParseBitrateStdout_ParsesFfprobe ------------------------------------

func TestParseBitrateStdout_ParsesFfprobe(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"1234567\n", 1234567, false},
		{"  999 \n", 999, false},
		{"1234,\n", 1234, false},
		{"", 0, true},
		{"abc\n", 0, true},
		{"-5\n", 0, true}, // non-positive
		{"0\n", 0, true},  // non-positive
	}
	for _, tc := range cases {
		got, err := parseBitrateStdout([]byte(tc.in))
		if tc.err {
			if err == nil {
				t.Errorf("parseBitrateStdout(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseBitrateStdout(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseBitrateStdout(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// --- TestWriteRenditionMetadata_SerializesJSON -------------------------------

func TestWriteRenditionMetadata_SerializesJSON(t *testing.T) {
	resetActiveRenditionsCache()
	t.Setenv("SIMLIVE_RENDITIONS", "")
	t.Setenv("SIMLIVE_QUALITY", "720p")

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForRendition(addr) {
		t.Skipf("redis not reachable at %s; skipping", addr)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := ctxForRendition()
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := fmt.Sprintf("test-rendition-meta-%d", time.Now().UnixNano())
	metaKey := fmt.Sprintf(renditionMetadataKey, chID)
	t.Cleanup(func() {
		ctx2, cancel2 := ctxForRendition()
		defer cancel2()
		rdb.Del(ctx2, metaKey)
	})

	p := &Planner{rdb: rdb}
	if err := p.writeRenditionMetadata(ctx, chID); err != nil {
		t.Fatalf("writeRenditionMetadata: %v", err)
	}

	raw, err := rdb.Get(ctx, metaKey).Result()
	if err != nil {
		t.Fatalf("GET simlive:renditions: %v", err)
	}
	var metas []RenditionMeta
	if err := json.Unmarshal([]byte(raw), &metas); err != nil {
		t.Fatalf("unmarshal RenditionMeta: %v (raw=%s)", err, raw)
	}
	// M1.5: writeRenditionMetadata always emits the full ladder
	// (360p / 720p / 1080p) regardless of SIMLIVE_QUALITY.
	if len(metas) != 3 {
		t.Fatalf("len(metas) = %d, want 3 (M1.5 full ladder)", len(metas))
	}
	// Order is ladder-sorted by the producer; assert ladder order directly.
	wantOrder := []string{"360p", "720p", "1080p"}
	for i, want := range wantOrder {
		if metas[i].Rendition != want {
			t.Fatalf("metas[%d].Rendition = %q, want %q (ladder order)", i, metas[i].Rendition, want)
		}
	}
	// Sanity-check per-tier fields: VOD/ads dirs follow the fallback map,
	// width/height match the tier target, bandwidth > 0 (probe or nominal).
	for i, m := range metas {
		var wantVod, wantAds string
		switch wantOrder[i] {
		case "360p":
			wantVod, wantAds = "L_0", "L2"
			if m.Width != 640 || m.Height != 360 {
				t.Fatalf("%s: size = %dx%d, want 640x360", m.Rendition, m.Width, m.Height)
			}
		case "720p":
			wantVod, wantAds = "L_1", "L1"
			if m.Width != 1280 || m.Height != 720 {
				t.Fatalf("%s: size = %dx%d, want 1280x720", m.Rendition, m.Width, m.Height)
			}
		case "1080p":
			wantVod, wantAds = "L_2", "L0"
			if m.Width != 1920 || m.Height != 1080 {
				t.Fatalf("%s: size = %dx%d, want 1920x1080", m.Rendition, m.Width, m.Height)
			}
		}
		if m.VodDir != wantVod {
			t.Fatalf("%s: VodDir = %q, want %q", m.Rendition, m.VodDir, wantVod)
		}
		if m.AdsDir != wantAds {
			t.Fatalf("%s: AdsDir = %q, want %q", m.Rendition, m.AdsDir, wantAds)
		}
		if m.Bandwidth <= 0 {
			t.Fatalf("%s: Bandwidth = %d, want > 0 (probe or nominal)", m.Rendition, m.Bandwidth)
		}
	}

	// JSON round-trip determinism: re-marshal and compare.
	bytes2, _ := json.Marshal(metas)
	if string(bytes2) != raw {
		t.Fatalf("re-marshal mismatch:\nfirst:  %s\nsecond: %s", raw, string(bytes2))
	}
}

// --- TestHasLegacyTimeline_ReportsAndSkips ----------------------------------

func TestHasLegacyTimeline_ReportsAndSkips(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForRendition(addr) {
		t.Skipf("redis not reachable at %s; skipping", addr)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := ctxForRendition()
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := fmt.Sprintf("test-legacy-timeline-%d", time.Now().UnixNano())
	// M2: hasLegacyTimeline checks the per-rung keys. Use the default ladder.
	keys := []string{
		timelineKeyFor(chID, "360p"),
		timelineKeyFor(chID, "720p"),
		timelineKeyFor(chID, "1080p"),
	}
	t.Cleanup(func() {
		ctx2, cancel2 := ctxForRendition()
		defer cancel2()
		rdb.Del(ctx2, keys...)
	})

	p := &Planner{rdb: rdb}

	// 1. Cold start: empty ZSETs -> has=false (don't flush empty).
	has, err := p.hasLegacyTimeline(ctx, chID)
	if err != nil {
		t.Fatalf("hasLegacyTimeline(empty): %v", err)
	}
	if has {
		t.Fatalf("hasLegacyTimeline(empty) = true, want false")
	}

	// 2. Seed a legacy member (no rendition field) in the 360p rung.
	legacy := []byte(`{"sequence":1,"storage_path":"x","duration":4,"media_start":100,"media_end":104}`)
	if err := rdb.ZAdd(ctx, keys[0], redis.Z{
		Score: 100, Member: string(legacy),
	}).Err(); err != nil {
		t.Fatalf("ZAdd legacy: %v", err)
	}
	has, err = p.hasLegacyTimeline(ctx, chID)
	if err != nil {
		t.Fatalf("hasLegacyTimeline(legacy): %v", err)
	}
	if !has {
		t.Fatalf("hasLegacyTimeline(legacy) = false, want true")
	}

	// 3. Seed a tagged member -> has=false (already M2-ready).
	if err := rdb.Del(ctx, keys...).Err(); err != nil {
		t.Fatalf("DEL: %v", err)
	}
	tagged := []byte(`{"sequence":2,"storage_path":"y","duration":4,"media_start":200,"media_end":204,"rendition":"360p"}`)
	if err := rdb.ZAdd(ctx, keys[0], redis.Z{
		Score: 200, Member: string(tagged),
	}).Err(); err != nil {
		t.Fatalf("ZAdd tagged: %v", err)
	}
	has, err = p.hasLegacyTimeline(ctx, chID)
	if err != nil {
		t.Fatalf("hasLegacyTimeline(tagged): %v", err)
	}
	if has {
		t.Fatalf("hasLegacyTimeline(tagged) = true, want false")
	}
}
