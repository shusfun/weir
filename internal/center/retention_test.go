package center

import (
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/sample"
)

func TestRetentionBoundsBacklogAndPreservesLiveData(t *testing.T) {
	s, _ := newInstalledServer(t)
	s.Config.Store.Maintenance.RecordBatch = 2
	ctx, now := t.Context(), time.Now()
	for i := 0; i < 5; i++ {
		s.Store.Ent.Sample.Create().SetScope(sample.ScopeGate).SetSubjectID(i).SetAt(now.Add(-48 * time.Hour)).SaveX(ctx)
		s.Store.Ent.Resource.Create().SetKind(observationKind).SetKey(itoa(i)).SetCreatedAt(now.Add(-30 * 24 * time.Hour)).SetData(map[string]any{}).SaveX(ctx)
	}
	s.Store.Ent.Sample.Create().SetScope(sample.ScopeGate).SetSubjectID(100).SetAt(now).SaveX(ctx)
	s.Store.Ent.Resource.Create().SetKind("authorization-journal").SetKey("pending").SetCreatedAt(now.Add(-30 * 24 * time.Hour)).SetData(map[string]any{}).SaveX(ctx)
	s.Store.Ent.Resource.Create().SetKind(observationPendingKind).SetKey("pending").SetCreatedAt(now.Add(-30 * 24 * time.Hour)).SetData(map[string]any{}).SaveX(ctx)
	if err := s.PruneSamples(ctx, now); err != nil {
		t.Fatal(err)
	}
	if n := s.Store.Ent.Sample.Query().Where(sample.AtLT(now.Add(-sampleKeep))).CountX(ctx); n != 3 {
		t.Fatalf("unbounded sample cleanup: %d", n)
	}
	if err := s.pruneTextObservations(ctx, now); err != nil {
		t.Fatal(err)
	}
	if n := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationKind)).CountX(ctx); n != 3 {
		t.Fatalf("unbounded observation cleanup: %d", n)
	}
	if n := s.Store.Ent.Resource.Query().Where(resource.KindEQ("authorization-journal")).CountX(ctx); n != 1 {
		t.Fatal("recovery fact was removed")
	}
	if n := s.Store.Ent.Resource.Query().Where(resource.KindEQ(observationPendingKind)).CountX(ctx); n != 1 {
		t.Fatal("pending observation was removed before recovery")
	}
	if n := s.Store.Ent.Sample.Query().Where(sample.AtGTE(now)).CountX(ctx); n != 1 {
		t.Fatal("live sample removed")
	}
}
