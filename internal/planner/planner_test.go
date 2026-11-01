package planner

import (
	"testing"
	"time"
)

func TestRecoveryCutoffUsesNowMinusBuffer(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	dayStart := now.Add(-20 * time.Hour)

	got := recoveryCutoff(now, dayStart, 120)
	want := now.Unix() - 120
	if got != want {
		t.Fatalf("recoveryCutoff = %d, want %d", got, want)
	}
}

func TestRecoveryCutoffNeverGoesBelowDayStart(t *testing.T) {
	dayStart := time.Unix(1_800_000_000, 0)
	now := dayStart.Add(30 * time.Second)

	got := recoveryCutoff(now, dayStart, 120)
	if got != dayStart.Unix() {
		t.Fatalf("recoveryCutoff = %d, want dayStart %d", got, dayStart.Unix())
	}
}

func TestParseFallbackState(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		expectedDay string
		wantOK      bool
	}{
		{
			name:        "valid",
			raw:         `{"target_date":"2026-10-30","source_date":"2026-10-29","day_start":100,"day_end":200}`,
			expectedDay: "2026-10-30",
			wantOK:      true,
		},
		{
			name:        "other day is ignored",
			raw:         `{"target_date":"2026-10-31","day_start":100,"day_end":200}`,
			expectedDay: "2026-10-30",
			wantOK:      false,
		},
		{
			name:        "malformed json",
			raw:         `{not-json`,
			expectedDay: "2026-10-30",
			wantOK:      false,
		},
		{
			name:        "invalid window",
			raw:         `{"target_date":"2026-10-30","day_start":200,"day_end":100}`,
			expectedDay: "2026-10-30",
			wantOK:      false,
		},
		{
			name:        "missing day window",
			raw:         `{"target_date":"2026-10-30"}`,
			expectedDay: "2026-10-30",
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := parseFallbackState([]byte(tt.raw), tt.expectedDay); ok != tt.wantOK {
				t.Fatalf("parseFallbackState ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}
