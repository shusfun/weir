package contracts

import (
	"errors"
	"time"
)

func (p TaskRequirements) Validate() error {
	if p.TargetGateID < 0 || p.PreferredGateID < 0 || len(p.Region) > 100 || len(p.Source) > 100 {
		return errors.New("invalid task requirements")
	}
	return nil
}

// Matches treats missing resource and reachability measurements as unknown.
// Unknown measurements cannot satisfy an explicit requirement.
func (p TaskRequirements) Matches(gateID int, kind string, version int, c CapabilityDocument, res WorkerResources) bool {
	if p.Validate() != nil || c.Tasks[kind] != version || p.TargetGateID != 0 && p.TargetGateID != gateID || p.Region != "" && p.Region != c.Region {
		return false
	}
	if p.Source != "" {
		if !c.Sources[p.Source] || res.SourcesObservedAt == nil || res.SourcesObservedAt.After(time.Now().Add(time.Minute)) || time.Since(*res.SourcesObservedAt) > 10*time.Minute {
			return false
		}
	}
	if p.MemoryBytes > 0 && (res.AvailableMemory == nil || *res.AvailableMemory < p.MemoryBytes) {
		return false
	}
	if p.DiskBytes > 0 && (res.AvailableDisk == nil || *res.AvailableDisk < p.DiskBytes) {
		return false
	}
	return true
}
