package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releaseSumOf(binary []byte) string {
	sum := sha256.Sum256(binary)
	return hex.EncodeToString(sum[:])
}

func TestInstallReleaseRejectsChecksumAndReplaysSameVersion(t *testing.T) {
	root := t.TempDir()
	restarts := 0
	restart := func() error { restarts++; return nil }
	binary := []byte("migate")
	sum := releaseSumOf(binary)
	if err := InstallRelease(root, "0.2.1", binary, strings.Repeat("0", 64), restart); err == nil || err.Error() != "release_checksum_mismatch" {
		t.Fatalf("mismatch err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "releases")); !os.IsNotExist(statErr) {
		t.Fatal("checksum failure must not create a release")
	}
	if err := InstallRelease(root, "0.2.1", binary, sum, restart); err != nil {
		t.Fatal(err)
	}
	if err := InstallRelease(root, "0.2.1", binary, sum, restart); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("restarts = %d, replay must not replace again", restarts)
	}
	for _, name := range []string{"migate-gate", "migate-manage"} {
		link, err := os.Readlink(filepath.Join(root, "releases", "0.2.1", name))
		if err != nil || link != "migate" {
			t.Fatalf("%s link = %q, %v", name, link, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "cores")); !os.IsNotExist(err) {
		t.Fatal("release replace must not create cores")
	}
}

func TestDecodeReleasePayloadRejectsURLBodyAndBadSum(t *testing.T) {
	good := map[string]any{"version": "0.2.1", "sha256": strings.Repeat("ab", 32)}
	if _, _, err := DecodeReleasePayload(good); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"url", "gate_body", "token"} {
		payload := map[string]any{"version": "0.2.1", "sha256": strings.Repeat("ab", 32), extra: "x"}
		if _, _, err := DecodeReleasePayload(payload); err == nil {
			t.Fatalf("%s payload must be refused", extra)
		}
	}
	if _, _, err := DecodeReleasePayload(map[string]any{"version": "0.2.1", "sha256": "ABC"}); err == nil {
		t.Fatal("malformed sum must be refused")
	}
	if _, _, err := DecodeReleasePayload(map[string]any{"version": "../x", "sha256": strings.Repeat("ab", 32)}); err == nil {
		t.Fatal("path-like version must be refused")
	}
}

func TestReleaseHandlerFailureIsNotApplied(t *testing.T) {
	root := t.TempDir()
	handler := releaseReplaceHandler(root, func() error { return os.ErrPermission })
	request := httptest.NewRequest("POST", "/release-replace", strings.NewReader("migate"))
	request.Header.Set("X-Release-Version", "0.2.1")
	request.Header.Set("X-Release-SHA256", strings.Repeat("ab", 32))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK || !strings.Contains(recorder.Body.String(), "release_checksum_mismatch") {
		t.Fatalf("status %d body %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "releases")); !os.IsNotExist(err) {
		t.Fatal("rejected release must not be written")
	}
}
