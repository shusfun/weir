package control

import "testing"

func TestUninstallDispatchKeepsOldGatePending(t *testing.T) {
	status, enqueue := UninstallDispatch(false, "retain", false)
	if status != "pending" || enqueue {
		t.Fatalf("old gate status=%s enqueue=%v", status, enqueue)
	}
	if _, enqueue = UninstallDispatch(true, "purge", false); enqueue {
		t.Fatal("purge without a second confirmation must not be sent")
	}
	status, enqueue = UninstallDispatch(true, "retain", false)
	if status != "pending" || !enqueue {
		t.Fatalf("capable retain status=%s enqueue=%v", status, enqueue)
	}
	if ShouldRevokeAfterUninstall("lifecycle.uninstall", "failed") {
		t.Fatal("failed uninstall must not revoke the device")
	}
	if !ShouldRevokeAfterUninstall("lifecycle.uninstall", "applied") {
		t.Fatal("applied uninstall should revoke the device")
	}
}
