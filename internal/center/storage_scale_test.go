package center

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
)

// Opt-in measurements compare the former N+1 projection with the same query
// and DTO using batch grants. They do not measure HTTP or browser latency.
func TestStorageScaleMeasurements(t *testing.T) {
	if os.Getenv("WEIR_STORAGE_SCALE") != "1" {
		t.Skip("opt-in bounded storage measurements")
	}
	for _, size := range []int{100, 1000, 10000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, _ := newInstalledServer(t)
			for first := 0; first < size; first += 100 {
				creates := make([]*ent.SubProfileCreate, 0, 100)
				for i := first; i < first+100; i++ {
					creates = append(creates, s.Store.Ent.SubProfile.Create().SetName(fmt.Sprintf("scale-%d", i)))
				}
				rows, err := s.Store.Ent.SubProfile.CreateBulk(creates...).Save(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				grants := make([]*ent.SubGrantCreate, 0, len(rows))
				for _, p := range rows {
					grants = append(grants, s.Store.Ent.SubGrant.Create().SetProfileID(p.ID).SetUserID(1).SetToken(fmt.Sprint(p.ID)))
				}
				if _, err = s.Store.Ent.SubGrant.CreateBulk(grants...).Save(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			for _, concurrency := range []int{1, 10, 30} {
				for _, mode := range []string{"legacy", "batch"} {
					t.Run(fmt.Sprintf("%s/c%d", mode, concurrency), func(t *testing.T) {
						ctx, cancel := context.WithTimeout(t.Context(), 290*time.Second)
						defer cancel()
						for round := 0; round < 3; round++ {
							cpuBefore, err := storageProcessCPU()
							if err != nil {
								t.Fatal(err)
							}
							var memBefore, memAfter runtime.MemStats
							runtime.ReadMemStats(&memBefore)
							beforePool := s.Store.DB.Stats()
							start := time.Now()
							values := make(chan time.Duration, concurrency)
							counts := make(chan int64, concurrency)
							errors := make(chan error, concurrency)
							var workers sync.WaitGroup
							for i := 0; i < concurrency; i++ {
								workers.Add(1)
								go func() {
									defer workers.Done()
									started := time.Now()
									work, metrics := store.ObserveQueries(ctx)
									rows, err := s.Store.Ent.SubProfile.Query().All(work)
									if err == nil {
										if mode == "legacy" {
											for _, p := range rows {
												if _, err = s.profileView(work, p); err != nil {
													break
												}
											}
										} else {
											_, err = s.profileViews(work, rows)
										}
									}
									if err != nil {
										errors <- err
									}
									values <- time.Since(started)
									counts <- metrics.Count.Load()
								}()
							}
							workers.Wait()
							close(values)
							close(counts)
							close(errors)
							for err := range errors {
								t.Fatal(err)
							}
							durations := []time.Duration{}
							for value := range values {
								durations = append(durations, value)
							}
							sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
							var queryCount int64
							for n := range counts {
								queryCount += n
							}
							runtime.ReadMemStats(&memAfter)
							afterPool := s.Store.DB.Stats()
							cpuAfter, err := storageProcessCPU()
							if err != nil {
								t.Fatal(err)
							}
							want := int64(2)
							if mode == "legacy" {
								want = int64(size + 1)
							}
							if queryCount != want*int64(concurrency) {
								t.Fatalf("query count %d", queryCount)
							}
							t.Logf("MEASURE size=%d concurrency=%d mode=%s round=%d sql_per_op=%d p50_ms=%.3f p95_ms=%.3f wall_ms=%.3f cpu_ms=%.3f allocated_bytes=%d heap_bytes=%d pool_wait_ms=%.3f", size, concurrency, mode, round+1, want, float64(durations[(len(durations)*50+99)/100-1])/1e6, float64(durations[(len(durations)*95+99)/100-1])/1e6, float64(time.Since(start))/1e6, (cpuAfter-cpuBefore)*1000, memAfter.TotalAlloc-memBefore.TotalAlloc, memAfter.HeapAlloc, float64(afterPool.WaitDuration-beforePool.WaitDuration)/1e6)
						}
					})
				}
			}
		})
	}
}
