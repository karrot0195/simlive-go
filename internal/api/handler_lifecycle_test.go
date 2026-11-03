package api

import (
	"testing"

	"simlive/internal/models"
)

func TestUpdateColumnsAndValuesTransition(t *testing.T) {
	req := &models.UpdateChannelReq{Status: strPtr("INACTIVE")}
	cols, vals := updateColumnsAndValues(req)
	if len(cols) != 2 {
		t.Fatalf("cols: %v", cols)
	}
	// status and is_active
	m := assignmentMap(cols, vals)
	if m["status"] != string(models.PlayoutInactiveStatus) {
		t.Fatalf("status: %v", m["status"])
	}
	if m["is_active"] != false {
		t.Fatalf("is_active: %v", m["is_active"])
	}

	req2 := &models.UpdateChannelReq{IsActive: boolPtr(true)}
	cols2, vals2 := updateColumnsAndValues(req2)
	m2 := assignmentMap(cols2, vals2)
	if m2["status"] != string(models.PlayoutPrepareStatus) {
		t.Fatalf("status: %v", m2["status"])
	}
	if m2["is_active"] != true {
		t.Fatalf("is_active: %v", m2["is_active"])
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
