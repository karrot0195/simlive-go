package models

import "testing"

func TestIsActivePredicate(t *testing.T) {
	cases := []struct {
		active bool
		status PlayoutStatus
		want   bool
	}{
		{true, PlayoutPrepareStatus, true},
		{true, PlayoutLIVEStatus, true},
		{true, PlayoutInactiveStatus, false},
		{true, PlayoutFailureStatus, false},
		{false, PlayoutPrepareStatus, false},
		{false, PlayoutLIVEStatus, false},
	}
	for _, c := range cases {
		if got := IsActivePredicate(c.active, c.status); got != c.want {
			t.Errorf("IsActivePredicate(%v,%s)=%v want %v", c.active, c.status, got, c.want)
		}
	}
}

func TestNormalizeLifecycle(t *testing.T) {
	// is_active=true only -> PREPARE,true
	s, ia, _ := NormalizeLifecycle(nil, boolPtr(true))
	if s != string(PlayoutPrepareStatus) || !ia {
		t.Fatalf("got %s %v", s, ia)
	}
	// is_active=false only -> INACTIVE,false
	s, ia, _ = NormalizeLifecycle(nil, boolPtr(false))
	if s != string(PlayoutInactiveStatus) || ia {
		t.Fatalf("got %s %v", s, ia)
	}
	// status=INACTIVE only -> inactive
	s2 := string(PlayoutInactiveStatus)
	s, ia, _ = NormalizeLifecycle(&s2, nil)
	if s != string(PlayoutInactiveStatus) || ia {
		t.Fatalf("got %s %v", s, ia)
	}
	// status=PREPARE only -> active
	s3 := string(PlayoutPrepareStatus)
	s, ia, _ = NormalizeLifecycle(&s3, nil)
	if s != string(PlayoutPrepareStatus) || !ia {
		t.Fatalf("got %s %v", s, ia)
	}
	// contradiction normalization: inactive+active -> PREPARE
	s4 := string(PlayoutInactiveStatus)
	s, ia, _ = NormalizeLifecycle(&s4, boolPtr(true))
	if s != string(PlayoutPrepareStatus) || !ia {
		t.Fatalf("got %s %v", s, ia)
	}
}

func TestValidateLifecycleContradiction(t *testing.T) {
	if err := ValidateLifecycleContradiction(strPtr("INACTIVE"), boolPtr(true)); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateLifecycleContradiction(strPtr("PREPARE"), boolPtr(false)); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateLifecycleContradiction(strPtr("PREPARE"), boolPtr(true)); err != nil {
		t.Fatal(err)
	}
}

func TestChannelDefinitionsEqual(t *testing.T) {
	a := PlayoutChannel{Name: "n", BaseName: "b", ContentTimezone: "tz", ChannelEpgID: "e", UseAdEpgAds: true}
	b := a
	if !ChannelDefinitionsEqual(a, b) {
		t.Fatal("expected equal")
	}
	b.Name = "x"
	if ChannelDefinitionsEqual(a, b) {
		t.Fatal("expected not equal")
	}
}

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }
