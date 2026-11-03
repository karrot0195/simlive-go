package lifecycle

import (
	"sort"

	"gorm.io/gorm"

	"simlive/internal/models"
)

// ActiveChannels returns the channels satisfying the platform-wide active
// predicate, ordered by id for stable diffs.
func ActiveChannels(db *gorm.DB) ([]models.PlayoutChannel, error) {
	var rows []models.PlayoutChannel
	err := db.Where("is_active = ? AND status NOT IN ?", true, []string{
		string(models.PlayoutInactiveStatus),
		string(models.PlayoutFailureStatus),
	}).Order("id").Find(&rows).Error
	return rows, err
}

// RosterDiff captures the difference between two active rosters.
type RosterDiff struct {
	Added                []models.PlayoutChannel
	Removed              []models.PlayoutChannel
	Unchanged            []models.PlayoutChannel
	DefinitionChanged    []models.PlayoutChannel
	StoppingReactivation []models.PlayoutChannel
}

// DiffRoster computes the diff between the previous and current active
// rosters, keyed by channel id.
func DiffRoster(previous, current []models.PlayoutChannel) RosterDiff {
	diff := RosterDiff{
		Added:                []models.PlayoutChannel{},
		Removed:              []models.PlayoutChannel{},
		Unchanged:            []models.PlayoutChannel{},
		DefinitionChanged:    []models.PlayoutChannel{},
		StoppingReactivation: []models.PlayoutChannel{},
	}
	prevByID := make(map[string]models.PlayoutChannel, len(previous))
	for _, ch := range previous {
		prevByID[ch.ID] = ch
	}
	currByID := make(map[string]models.PlayoutChannel, len(current))
	for _, ch := range current {
		currByID[ch.ID] = ch
	}

	for _, ch := range current {
		if prev, ok := prevByID[ch.ID]; ok {
			if models.ChannelDefinitionsEqual(prev, ch) {
				diff.Unchanged = append(diff.Unchanged, ch)
			} else {
				diff.DefinitionChanged = append(diff.DefinitionChanged, ch)
			}
		} else {
			diff.Added = append(diff.Added, ch)
		}
	}
	for _, ch := range previous {
		if _, ok := currByID[ch.ID]; !ok {
			diff.Removed = append(diff.Removed, ch)
		}
	}
	// stable ordering by id
	for _, s := range [][]models.PlayoutChannel{diff.Added, diff.Removed, diff.Unchanged, diff.DefinitionChanged} {
		sort.Slice(s, func(i, j int) bool { return s[i].ID < s[j].ID })
	}
	return diff
}

// DesiredState is a convenience alias returning the set of active channel
// ids for a roster.
func DesiredState(channels []models.PlayoutChannel) map[string]struct{} {
	out := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		out[ch.ID] = struct{}{}
	}
	return out
}
