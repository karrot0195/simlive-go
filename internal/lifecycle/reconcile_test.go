package lifecycle

import (
	"testing"

	"simlive/internal/models"
)

func TestDiffRoster(t *testing.T) {
	prev := []models.PlayoutChannel{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}
	curr := []models.PlayoutChannel{{ID: "1", Name: "a"}, {ID: "3", Name: "c"}}
	d := DiffRoster(prev, curr)
	if len(d.Added) != 1 || d.Added[0].ID != "3" {
		t.Fatalf("added: %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ID != "2" {
		t.Fatalf("removed: %+v", d.Removed)
	}
	if len(d.Unchanged) != 1 || d.Unchanged[0].ID != "1" {
		t.Fatalf("unchanged: %+v", d.Unchanged)
	}
}

func TestDiffRosterDefinitionChanged(t *testing.T) {
	prev := []models.PlayoutChannel{{ID: "1", Name: "a"}}
	curr := []models.PlayoutChannel{{ID: "1", Name: "b"}}
	d := DiffRoster(prev, curr)
	if len(d.DefinitionChanged) != 1 {
		t.Fatalf("defchanged: %+v", d.DefinitionChanged)
	}
}

func TestActiveChannelsQuery(t *testing.T) {
	// smoke: function exists
	_ = ActiveChannels
}
