package api

import "testing"

func TestSelectFallbackStateActiveDay(t *testing.T) {
	values := map[string]string{
		"2026-10-30": `{"target_date":"2026-10-30","source_date":"2026-10-29","day_start":1000,"day_end":2000}`,
	}

	active, sourceDate := selectFallbackState(values, 1500)
	if !active {
		t.Fatalf("expected fallback to be active inside the day window")
	}
	if sourceDate != "2026-10-29" {
		t.Fatalf("source date = %q, want 2026-10-29", sourceDate)
	}
}

func TestSelectFallbackStateIgnoresOtherDays(t *testing.T) {
	values := map[string]string{
		"2026-10-31": `{"target_date":"2026-10-31","source_date":"2026-10-30","day_start":2000,"day_end":3000}`,
	}

	if active, _ := selectFallbackState(values, 1500); active {
		t.Fatalf("future fallback day must not label the current day")
	}
}

func TestSelectFallbackStateSkipsInvalidEntries(t *testing.T) {
	values := map[string]string{
		"malformed": `{not-json`,
		"badWindow": `{"source_date":"2026-10-29","day_start":2000,"day_end":1000}`,
		"noWindow":  `{"source_date":"2026-10-29"}`,
	}

	if active, _ := selectFallbackState(values, 1500); active {
		t.Fatalf("invalid entries must not activate the label")
	}
}

func TestSelectFallbackStateEmpty(t *testing.T) {
	if active, sourceDate := selectFallbackState(nil, 1500); active || sourceDate != "" {
		t.Fatalf("expected no fallback for an empty hash, got active=%v source=%q", active, sourceDate)
	}
}
