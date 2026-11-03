package models

import "fmt"

// IsActiveChannel implements the platform-wide active predicate:
//
//	is_active == true AND status NOT IN ('INACTIVE', 'FAILURE')
func IsActiveChannel(c PlayoutChannel) bool {
	return IsActivePredicate(c.IsActive, c.Status)
}

// IsActivePredicate is the pure form of the active predicate, taking the
// raw column values.
func IsActivePredicate(isActive bool, status PlayoutStatus) bool {
	if !isActive {
		return false
	}
	if status == PlayoutInactiveStatus || status == PlayoutFailureStatus {
		return false
	}
	return true
}

// ChannelIsActive is an alias for IsActivePredicate kept for callers that
// prefer the "Channel" naming.
func ChannelIsActive(isActive bool, status PlayoutStatus) bool {
	return IsActivePredicate(isActive, status)
}

// NormalizeLifecycle applies the status/is_active normalization rules:
//
//	is_active=true + status=INACTIVE  -> status=PREPARE
//	is_active=false + status=PREPARE|LIVE -> status=INACTIVE
//
// When only one of the two is provided (the other is nil), the missing
// value is derived. Returns the normalized status and is_active.
func NormalizeLifecycle(status *string, isActive *bool) (string, bool, bool) {
	s := ""
	if status != nil {
		s = *status
	}
	ia := false
	hasIA := isActive != nil
	if hasIA {
		ia = *isActive
	}
	hasStatus := status != nil

	// Derive missing counterpart.
	if hasStatus && !hasIA {
		switch s {
		case string(PlayoutPrepareStatus), string(PlayoutLIVEStatus):
			ia = true
		case string(PlayoutInactiveStatus):
			ia = false
		case string(PlayoutFailureStatus):
			ia = false
		}
		hasIA = true
	}
	if hasIA && !hasStatus {
		if ia {
			s = string(PlayoutPrepareStatus)
		} else {
			s = string(PlayoutInactiveStatus)
		}
		hasStatus = true
	}

	// Normalize contradictory-ish single-side: inactive+active
	if ia && s == string(PlayoutInactiveStatus) {
		s = string(PlayoutPrepareStatus)
	}
	if !ia && (s == string(PlayoutPrepareStatus) || s == string(PlayoutLIVEStatus)) {
		s = string(PlayoutInactiveStatus)
	}
	return s, ia, true
}

// ValidateLifecycleContradiction reports an error when the requested status
// and is_active values contradict each other per the contract transition
// table.
func ValidateLifecycleContradiction(status *string, isActive *bool) error {
	if status == nil || isActive == nil {
		return nil
	}
	s := *status
	ia := *isActive
	if s == string(PlayoutInactiveStatus) && ia {
		return fmt.Errorf("contradiction: status=INACTIVE with is_active=true")
	}
	if (s == string(PlayoutPrepareStatus) || s == string(PlayoutLIVEStatus)) && !ia {
		return fmt.Errorf("contradiction: status=%s with is_active=false", s)
	}
	return nil
}

// ChannelDefinitionsEqual reports whether two channels have the same
// definition (name, base_name, content_timezone, channel_epg_id,
// use_ad_epg_ads).
func ChannelDefinitionsEqual(a, b PlayoutChannel) bool {
	return a.Name == b.Name &&
		a.BaseName == b.BaseName &&
		a.ContentTimezone == b.ContentTimezone &&
		a.ChannelEpgID == b.ChannelEpgID &&
		a.UseAdEpgAds == b.UseAdEpgAds
}

// ChannelDefinitionEqual is an alias.
func ChannelDefinitionEqual(a, b PlayoutChannel) bool {
	return ChannelDefinitionsEqual(a, b)
}
