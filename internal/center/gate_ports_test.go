package center

import "testing"

func TestStatsPortAllocationReadsCurrentGate(t *testing.T) {
	s, _, fake := setupGate(t)
	ctx := t.Context()
	stale := s.Store.Ent.Gate.GetX(ctx, int(fake.id))
	first, err := s.ensureStatsPorts(ctx, stale)
	if err != nil {
		t.Fatal(err)
	}
	if first.XrayStatsPort == 0 || first.SingboxStatsPort == 0 || first.XrayStatsPort == first.SingboxStatsPort {
		t.Fatal("invalid stats allocation")
	}
	second, err := s.ensureStatsPorts(ctx, stale)
	if err != nil {
		t.Fatal(err)
	}
	if first.XrayStatsPort != second.XrayStatsPort || first.SingboxStatsPort != second.SingboxStatsPort || first.ConfigRevision != second.ConfigRevision {
		t.Fatal("stale caller reallocated current ports")
	}
	// The returned entity must remain usable after its allocation transaction.
	if _, err := second.Update().SetName("after stats allocation").Save(ctx); err != nil {
		t.Fatal(err)
	}
}
