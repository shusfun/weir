package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/audit"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/packages/contracts"
)

func testReceiptTransaction(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	task, err := s.Store.Task.Create().SetDeviceID(4242).SetOperation("certificate.upsert").SetKey("receipt-atomicity").SetRevision(17).SetStatus("running").SetExpiresAt(time.Now().Add(time.Minute)).SetLeasedUntil(time.Now()).SetPayload(map[string]any{"id": 1, "certificate_pem": "public", "sealed_key": "never-resource"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reject := true
	s.Store.Resource.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if rm, ok := m.(*ent.ResourceMutation); ok {
				kind, _ := rm.Kind()
				if reject && kind == "device-certificate" {
					return nil, errors.New("injected resource write failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	receipt := func() int {
		body, _ := json.Marshal(contracts.Result{TaskID: task.ID, Revision: 17, Status: "applied"})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/receipt", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("device_id", 4242)
		s.taskResult(c)
		return w.Code
	}
	if receipt() != 500 {
		t.Fatal("injected resource failure not returned")
	}
	saved, _ := s.Store.Task.Get(ctx, task.ID)
	if saved.Status != "running" {
		t.Fatal("receipt committed before resource")
	}
	reject = false
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if receipt() != 200 {
				t.Error("duplicate receipt failed")
			}
		}()
	}
	wg.Wait()
	saved, _ = s.Store.Task.Get(ctx, task.ID)
	r, err := s.Store.Resource.Query().Where(resource.KindEQ("device-certificate"), resource.KeyEQ("4242:1")).Only(ctx)
	if saved.Status != "applied" || err != nil || r.Data["sealed_key"] != nil || r.Data["key_pem"] != nil {
		t.Fatal("invalid atomic public certificate resource")
	}
	count, err := s.Store.Audit.Query().Where(audit.ActionEQ("task.result"), audit.TargetEQ(strconv.Itoa(saved.ID))).Count(ctx)
	if err != nil || count != 1 {
		t.Fatal("duplicate receipt audit", count, err)
	}
}
