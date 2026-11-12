package accountplan

import (
	"errors"
	"math"
	"slices"
	"time"
)

// Grant is an immutable issuance snapshot. Editing a plan never changes codes
// that have already been handed to someone.
type Grant struct {
	Quota      int64 `json:"quota"`
	Unlimited  bool  `json:"unlimited"`
	Days       int   `json:"days"`
	AllNodes   bool  `json:"all_nodes"`
	NodeIDs    []int `json:"node_ids"`
	ProfileIDs []int `json:"profile_ids"`
}

func (g Grant) Validate() error {
	if g.Quota < 0 || g.Days < 0 || g.Days > 3650 || len(g.NodeIDs) > 1000 || len(g.ProfileIDs) > 1000 {
		return errors.New("invalid_plan_grant")
	}
	for _, ids := range [][]int{g.NodeIDs, g.ProfileIDs} {
		seen := map[int]bool{}
		for _, id := range ids {
			if id < 1 || seen[id] {
				return errors.New("invalid_plan_grant")
			}
			seen[id] = true
		}
	}
	return nil
}

type Account struct {
	Quota     int64
	Unlimited bool
	ExpiresAt *time.Time
	AllNodes  bool
	NodeIDs   []int
}

func Apply(a Account, g Grant, now time.Time, initial bool) (Account, error) {
	if err := g.Validate(); err != nil {
		return a, err
	}
	if a.Quota < 0 || g.Quota > math.MaxInt64-a.Quota {
		return a, errors.New("plan_quota_overflow")
	}
	a.Quota += g.Quota
	a.Unlimited = a.Unlimited || g.Unlimited
	a.AllNodes = a.AllNodes || g.AllNodes
	a.NodeIDs = union(a.NodeIDs, g.NodeIDs)
	// Existing perpetual accounts must not lose their perpetual validity.
	if g.Days > 0 && (initial || a.ExpiresAt != nil) {
		start := now
		if a.ExpiresAt != nil && a.ExpiresAt.After(start) {
			start = *a.ExpiresAt
		}
		end := start.AddDate(0, 0, g.Days)
		if end.Year() > 9999 {
			return a, errors.New("plan_expiry_overflow")
		}
		a.ExpiresAt = &end
	}
	return a, nil
}

func union(a, b []int) []int {
	result := append(slices.Clone(a), b...)
	slices.Sort(result)
	return slices.Compact(result)
}
