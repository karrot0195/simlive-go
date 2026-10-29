package utils

import (
	"os"
	"testing"
)

func TestLocalLocationPrefersTZEnv(t *testing.T) {
	old, had := os.LookupEnv("TZ")
	os.Setenv("TZ", "Asia/Saigon")
	defer func() {
		if had {
			os.Setenv("TZ", old)
		} else {
			os.Unsetenv("TZ")
		}
	}()

	loc := LocalLocation()
	if loc.String() != "Asia/Saigon" {
		t.Fatalf("expected Asia/Saigon from TZ env, got %q", loc.String())
	}
	if got := LocalTZOffset(); got != "+07:00" {
		t.Fatalf("expected +07:00, got %q", got)
	}
}

func TestLocalTZOffsetFallback(t *testing.T) {
	os.Unsetenv("TZ")
	loc := LocalLocation()
	if loc == nil {
		t.Fatal("LocalLocation returned nil with no TZ env")
	}
	// Offset must still parse as ±HH:MM regardless of which zone is active.
	off := LocalTZOffset()
	if len(off) != 6 || (off[0] != '+' && off[0] != '-') {
		t.Fatalf("LocalTZOffset did not return ±HH:MM, got %q", off)
	}
}