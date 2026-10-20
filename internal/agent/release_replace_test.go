package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releasePayload(version string, gate, manage []byte) map[string]any {
	gateSum := sha256.Sum256(gate)
	manageSum := sha256.Sum256(manage)
	return map[string]any{
		"version":       version,
		"gate_sha256":   hex.EncodeToString(gateSum[:]),
		"manage_sha256": hex.EncodeToString(manageSum[:]),
		"gate_body":     base64.StdEncoding.EncodeToString(gate),
		"manage_body":   base64.StdEncoding.EncodeToString(manage),
	}
}

func TestInstallReleaseRejectsChecksumAndReplaysSameVersion(t *testing.T) {
	root := t.TempDir()
	restarts := 0
	restart := func() error { restarts++; return nil }
	payload := releasePayload("0.2.1", []byte("gate"), []byte("manage"))
	version, gate, manage, gateSum, manageSum, err := DecodeReleasePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = InstallRelease(root, version, gate, manage, "0000000000000000000000000000000000000000000000000000000000000000", manageSum, restart); err == nil || err.Error() != "release_checksum_mismatch" {
		t.Fatalf("mismatch err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "releases")); !os.IsNotExist(statErr) {
		t.Fatal("checksum failure must not create a release")
	}
	if err = InstallRelease(root, version, gate, manage, gateSum, manageSum, restart); err != nil {
		t.Fatal(err)
	}
	if err = InstallRelease(root, version, gate, manage, gateSum, manageSum, restart); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("restarts = %d, replay must not replace again", restarts)
	}
	if _, err = os.Stat(filepath.Join(root, "cores")); !os.IsNotExist(err) {
		t.Fatal("release replace must not create cores")
	}
}

func TestDecodeReleasePayloadRejectsURLAndToken(t *testing.T) {
	payload := releasePayload("0.2.1", []byte("gate"), []byte("manage"))
	payload["url"] = "https://github.com/shusfun/MiSub/releases/download/v0.2.1/file"
	if _, _, _, _, _, err := DecodeReleasePayload(payload); err == nil {
		t.Fatal("url payload must be refused")
	}
}

func TestReleaseHandlerFailureIsNotApplied(t *testing.T) {
	root := t.TempDir()
	handler := releaseReplaceHandler(root, func() error { return os.ErrPermission })
	payload := releasePayload("0.2.1", []byte("gate"), []byte("manage"))
	payload["gate_sha256"] = strings.Repeat("ab", 32)
	body := `{"version":"0.2.1","gate_sha256":"` + strings.Repeat("ab", 32) + `","manage_sha256":"` + payload["manage_sha256"].(string) + `","gate_body":"` + payload["gate_body"].(string) + `","manage_body":"` + payload["manage_body"].(string) + `"}`
	request := httptest.NewRequest("POST", "/release-replace", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK || !strings.Contains(recorder.Body.String(), "release_checksum_mismatch") {
		t.Fatalf("status %d body %s", recorder.Code, recorder.Body.String())
	}
}
