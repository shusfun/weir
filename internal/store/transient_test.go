package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryTransientExpiresAndDoesNotSurviveNewInstance(t *testing.T) {
	m := &memoryTransient{}
	if err := m.Set(t.Context(), "observation", "value", time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := m.Get(t.Context(), "observation"); err != nil || !ok || got != "value" {
		t.Fatal(got, ok, err)
	}
	if _, ok, _ := (&memoryTransient{}).Get(t.Context(), "observation"); ok {
		t.Fatal("soft state survived new store")
	}
	m.mu.Lock()
	v := m.values["observation"]
	v.until = time.Now().Add(-time.Second)
	m.values["observation"] = v
	m.mu.Unlock()
	if _, ok, _ := m.Get(t.Context(), "observation"); ok {
		t.Fatal("expired observation returned")
	}
	if err := m.Set(t.Context(), "oversize", strings.Repeat("x", (256<<10)+1), time.Minute); err == nil {
		t.Fatal("unbounded observation accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.Set(ctx, "cancelled", "value", time.Minute); err == nil {
		t.Fatal("cancelled write accepted")
	}
}
