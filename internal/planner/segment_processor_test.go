package planner

import (
	"testing"
	"time"

	"simlive/internal/models"
	"simlive/internal/utils"
)

func TestAdBreakForProgramme(t *testing.T) {
	if ctx := adBreakForProgramme(false, 1000, 1030); ctx != nil {
		t.Fatalf("content programme must not create an ad break, got %+v", ctx)
	}
	ctx := adBreakForProgramme(true, 1000, 1030)
	if ctx == nil {
		t.Fatal("ad programme must create an ad break")
	}
	if ctx.id != 1000 {
		t.Fatalf("break id = %d, want 1000 (stable from start)", ctx.id)
	}
	if ctx.start != 1000 || ctx.duration != 30 {
		t.Fatalf("break window = %.0f/%.0f, want 1000/30", ctx.start, ctx.duration)
	}
}

func TestApplyAdMarkerFirstAndLast(t *testing.T) {
	ctx := adBreakForProgramme(true, 1000, 1030)

	first := models.SegmentItem{}
	applyAdMarker(&first, ctx, true, false)
	if !first.IsAd || first.AdBreakID != 1000 || first.AdBreakStart != 1000 || first.AdBreakDuration != 30 {
		t.Fatalf("first ad segment markers wrong: %+v", first)
	}
	if first.AdType != "out" {
		t.Fatalf("first segment AdType = %q, want out", first.AdType)
	}
	if !hasSpliceType(first.SliceInserts, "out") {
		t.Fatal("first segment must carry an out splice insert")
	}

	last := models.SegmentItem{}
	applyAdMarker(&last, ctx, false, true)
	if last.AdType != "in" {
		t.Fatalf("last segment AdType = %q, want in", last.AdType)
	}
	if !hasSpliceType(last.SliceInserts, "in") {
		t.Fatal("last segment must carry an in splice insert")
	}
}

func TestApplyAdMarkerMiddleSegment(t *testing.T) {
	ctx := adBreakForProgramme(true, 1000, 1030)
	mid := models.SegmentItem{}
	applyAdMarker(&mid, ctx, false, false)
	if !mid.IsAd || mid.AdBreakID != 1000 {
		t.Fatalf("middle ad segment markers wrong: %+v", mid)
	}
	if mid.AdType != "" {
		t.Fatalf("middle segment AdType = %q, want empty", mid.AdType)
	}
}

func TestApplyAdMarkerNoBreakIsNoop(t *testing.T) {
	seg := models.SegmentItem{}
	applyAdMarker(&seg, nil, true, true)
	if seg.IsAd || seg.AdBreakID != 0 || seg.AdType != "" {
		t.Fatalf("nil ad break must not mark the segment: %+v", seg)
	}
}

func hasSpliceType(inserts []models.SpliceInsert, typ string) bool {
	for _, s := range inserts {
		if s.Type == typ {
			return true
		}
	}
	return false
}

func TestAnchorTimelineStart(t *testing.T) {
	now := time.Date(2026, 11, 2, 16, 44, 0, 0, time.Local)
	buffer := float64(utils.GetPlannerSettings().WindowBufferSeconds)
	liveEdge := float64(now.Unix()) - buffer

	t.Run("fresh plan mid-programme starts at live edge", func(t *testing.T) {
		start := float64(time.Date(2026, 11, 2, 13, 39, 0, 0, time.Local).Unix())
		end := time.Date(2026, 11, 2, 18, 13, 0, 0, time.Local)
		if got := anchorTimelineStart(start, end, now, 0); got != liveEdge {
			t.Fatalf("got %.0f, want live edge %.0f", got, liveEdge)
		}
	})

	t.Run("future programme keeps its own start", func(t *testing.T) {
		start := float64(time.Date(2026, 11, 2, 18, 13, 0, 0, time.Local).Unix())
		end := time.Date(2026, 11, 2, 19, 39, 0, 0, time.Local)
		if got := anchorTimelineStart(start, end, now, 0); got != start {
			t.Fatalf("got %.0f, want programme start %.0f", got, start)
		}
	})

	t.Run("already planned tail is kept", func(t *testing.T) {
		start := float64(time.Date(2026, 11, 2, 13, 39, 0, 0, time.Local).Unix())
		end := time.Date(2026, 11, 2, 18, 13, 0, 0, time.Local)
		tail := liveEdge + 600
		if got := anchorTimelineStart(start, end, now, tail); got != tail {
			t.Fatalf("got %.0f, want existing tail %.0f", got, tail)
		}
	})

	t.Run("live edge past the item end does not apply", func(t *testing.T) {
		start := float64(time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local).Unix())
		end := time.Date(2026, 11, 2, 11, 0, 0, 0, time.Local)
		if got := anchorTimelineStart(start, end, now, 0); got != start {
			t.Fatalf("got %.0f, want programme start %.0f", got, start)
		}
	})
}

// The XML EPG's stream_id carries the storage bucket (rclone
// remote:bucket/... form), so the storage base is the stream id itself.
func TestStreamStorageBaseUsesBucketInclusiveStreamID(t *testing.T) {
	const id = "espx-video-lib:espx-video-lib/vod/ch-1/prog-1"
	stream := models.PartnerProgrammeStream{StreamID: id}
	if got := streamStorageBase(stream); got != id {
		t.Fatalf("streamStorageBase = %q, want %q", got, id)
	}

	withPath := models.PartnerProgrammeStream{StreamID: id, StreamPath: "sub/dir"}
	if got := streamStorageBase(withPath); got != "sub/dir/"+id {
		t.Fatalf("streamStorageBase with StreamPath = %q, want %q", got, "sub/dir/"+id)
	}
}

func TestSegmentStoragePathFromBucketInclusiveStreamID(t *testing.T) {
	const id = "espx-video-lib:espx-video-lib/vod/ch-1/prog-1"
	want := id + "/{rendition}/seg_00001.ts"
	if got := segmentStoragePath(id, "seg_00001.ts"); got != want {
		t.Fatalf("segmentStoragePath = %q, want %q", got, want)
	}
}
