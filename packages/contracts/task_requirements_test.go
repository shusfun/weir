package contracts

import (
	"testing"
	"time"
)

func TestTaskRequirementsRejectUnknownAndInsufficientResources(t *testing.T) {
	mem, disk := uint64(4096), uint64(8192)
	now := time.Now().UTC()
	c := CapabilityDocument{Tasks: map[string]int{TaskLayaInstall: TaskVersion}, Region: "jp", Sources: map[string]bool{"github": true}}
	p := TaskRequirements{TargetGateID: 7, PreferredGateID: 8, Region: "jp", Source: "github", MemoryBytes: mem, DiskBytes: disk}
	r := WorkerResources{AvailableMemory: &mem, AvailableDisk: &disk, Concurrency: 1, SourcesObservedAt: &now}
	if !p.Matches(7, TaskLayaInstall, TaskVersion, c, r) {
		t.Fatal("matching bound task rejected")
	}
	for _, change := range []struct {
		name  string
		apply func(*CapabilityDocument, *WorkerResources)
	}{
		{"memory unknown", func(_ *CapabilityDocument, r *WorkerResources) { r.AvailableMemory = nil }},
		{"disk unknown", func(_ *CapabilityDocument, r *WorkerResources) { r.AvailableDisk = nil }},
		{"memory insufficient", func(_ *CapabilityDocument, r *WorkerResources) { v := mem - 1; r.AvailableMemory = &v }},
		{"region mismatch", func(c *CapabilityDocument, _ *WorkerResources) { c.Region = "us" }},
		{"source observation missing", func(_ *CapabilityDocument, r *WorkerResources) { r.SourcesObservedAt = nil }},
		{"source observation stale", func(_ *CapabilityDocument, r *WorkerResources) {
			at := now.Add(-11 * time.Minute)
			r.SourcesObservedAt = &at
		}},
		{"source unknown", func(c *CapabilityDocument, _ *WorkerResources) { c.Sources = nil }},
		{"source unreachable", func(c *CapabilityDocument, _ *WorkerResources) { c.Sources = map[string]bool{"github": false} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			cc, rr := c, r
			change.apply(&cc, &rr)
			if p.Matches(7, TaskLayaInstall, TaskVersion, cc, rr) {
				t.Fatal("unsatisfied requirement accepted")
			}
		})
	}
	if p.Matches(8, TaskLayaInstall, TaskVersion, c, r) {
		t.Fatal("resource-bound task moved to preferred gate")
	}
	if p.Matches(7, TaskLayaInstall, TaskVersion+1, c, r) {
		t.Fatal("unsupported version accepted")
	}
}
