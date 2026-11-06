package store

import (
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store/ent/sample"
	"github.com/shusfun/weir/packages/contracts"
)

func TestSampleObservationIncrementalMigration(t *testing.T) {
	for name, open := range backends(t) {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := t.Context()
			legacy, err := s.Ent.Sample.Create().SetScope(sample.ScopeGate).SetSubjectID(7).SetAt(time.Now().Truncate(time.Minute)).SetTraffic(42).SetCPU(25).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			pull, err := s.Ent.SubPull.Create().SetLinkID(7).SetUserID(3).SetTarget("singbox").Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, "ALTER TABLE sub_pulls DROP COLUMN trace_id"); err != nil {
				t.Fatal(err)
			}
			for _, column := range []string{"processes", "observation_id", "trace_id"} {
				if _, err := s.DB.ExecContext(ctx, "ALTER TABLE samples DROP COLUMN "+column); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Ent.Schema.Create(ctx); err != nil {
				t.Fatal(err)
			}
			gotPull, err := s.Ent.SubPull.Get(ctx, pull.ID)
			if err != nil || gotPull.Target != "singbox" || gotPull.TraceID != "" {
				t.Fatalf("legacy pull=%+v err=%v", gotPull, err)
			}
			if err := gotPull.Update().SetTraceID("0123456789abcdef0123456789abcdef").Exec(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := s.Ent.Sample.Get(ctx, legacy.ID)
			if err != nil || got.Traffic != 42 || got.CPU == nil || *got.CPU != 25 || got.ObservationID != "" || got.TraceID != "" || len(got.Processes) != 0 {
				t.Fatalf("legacy=%+v err=%v", got, err)
			}
			_, err = s.Ent.Sample.Create().SetScope(sample.ScopeCenter).SetSubjectID(1).SetAt(time.Now().Truncate(time.Minute)).SetProcesses(map[string]contracts.ProcessSample{"weir-center": {State: "running"}}).SetObservationID("observation").SetTraceID("trace").Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
