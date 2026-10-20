package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUninstallCommandKeepsModesSeparate(t *testing.T) {
	retain, err := UninstallCommand("retain")
	if err != nil || strings.Contains(strings.Join(retain, " "), "purge") || strings.Contains(strings.Join(retain, " "), "with-cores") {
		t.Fatalf("retain = %v %v", retain, err)
	}
	cores, err := UninstallCommand("cores")
	if err != nil || !strings.Contains(strings.Join(cores, " "), "gate:uninstall:with-cores") || strings.Contains(strings.Join(cores, " "), "--purge") {
		t.Fatalf("cores = %v %v", cores, err)
	}
	purge, err := UninstallCommand("purge")
	if err != nil || !strings.Contains(strings.Join(purge, " "), "gate:uninstall:purge") || !strings.Contains(strings.Join(purge, " "), "/var/backups/migate") {
		t.Fatalf("purge = %v %v", purge, err)
	}
	if _, err = UninstallCommand("shell"); err == nil {
		t.Fatal("arbitrary mode must be refused")
	}
}

func TestUninstallHandlerFailureDoesNotReportApplied(t *testing.T) {
	handler := uninstallHandler(func(args []string) error {
		t.Fatalf("handler should reject extra fields before running: %v", args)
		return nil
	})
	request := httptest.NewRequest(http.MethodPost, "/uninstall", strings.NewReader(`{"mode":"retain","command":"rm -rf /"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatal("extra payload must not succeed")
	}
}
