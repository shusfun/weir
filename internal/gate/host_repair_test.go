package gate

import (
	"context"
	"testing"
)

func TestHostRepairRejectsUnlistedKey(t *testing.T) {
	h := &Helper{P: Paths{Data: t.TempDir()}}
	if _, err := h.repairHost(context.Background(), "net.ipv4.ip_forward", "1"); err == nil {
		t.Fatal("unlisted sysctl was accepted")
	}
}
