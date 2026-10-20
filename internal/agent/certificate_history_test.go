package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func historyFixture(t *testing.T) (certificateHistoryManager, []string) {
	t.Helper()
	root := t.TempDir()
	m := certificateHistoryManager{filepath.Join(root, "certs"), filepath.Join(root, "cores")}
	for _, dir := range []string{m.root, m.config} {
		if err := os.Mkdir(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{}
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("1-%032x", i)
		names = append(names, name)
		cert := ManagedCertificate{ID: 1, Certificate: fmt.Sprintf("test-certificate-%d", i), Key: fmt.Sprintf("test-private-key-%d", i)}
		for ext, body := range map[string]string{".pem": cert.Certificate, ".key": cert.Key} {
			if err := os.WriteFile(filepath.Join(m.root, name+ext), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.register(cert, filepath.Join(m.root, name)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := m.load()
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		v := r.Versions[name]
		v.Created = time.Unix(int64(i+1), 0)
		r.Versions[name] = v
	}
	if err = m.save(r); err != nil {
		t.Fatal(err)
	}
	return m, names
}
func historyDigest(t *testing.T, m certificateHistoryManager) string {
	t.Helper()
	v, err := m.inspect()
	if err != nil {
		t.Fatal(err)
	}
	return v["preview_digest"].(string)
}
func TestCertificateHistoryOwnershipProtectionAndReplay(t *testing.T) {
	m, names := historyFixture(t)
	unknown := filepath.Join(m.root, "foreign.key")
	os.WriteFile(unknown, []byte("other-application"), 0600)
	b, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"certificateFile": filepath.Join(m.root, names[0]+".pem")}}})
	os.WriteFile(filepath.Join(m.config, "xray.json"), b, 0600)
	view, err := m.inspect()
	if err != nil || view["removable"] != 1 {
		t.Fatal("current config and newest two not protected", err)
	}
	result, err := m.clean(10, view["preview_digest"].(string))
	if err != nil || result["removed"] != 1 {
		t.Fatal("cleanup", err)
	}
	for _, name := range []string{names[0], names[2], names[3]} {
		if _, err = os.Stat(filepath.Join(m.root, name+".key")); err != nil {
			t.Fatal("protected key deleted")
		}
	}
	if _, err = os.Stat(filepath.Join(m.root, names[1]+".key")); !os.IsNotExist(err) {
		t.Fatal("retired key retained")
	}
	if body, err := os.ReadFile(unknown); err != nil || string(body) != "other-application" {
		t.Fatal("unregistered file touched")
	}
	if _, err = m.clean(10, view["preview_digest"].(string)); err != nil {
		t.Fatal("completed replay failed", err)
	}
	if _, err = m.clean(10, coreHash([]byte("different"))); err == nil {
		t.Fatal("replay conflict accepted")
	}
	if _, err = m.clean(9, historyDigest(t, m)); err == nil {
		t.Fatal("stale task accepted")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "test-private") {
		t.Fatal("key leaked in view")
	}
}
func TestCertificateHistoryRejectsChangedAndUnsafeFiles(t *testing.T) {
	m, names := historyFixture(t)
	digest := historyDigest(t, m)
	b, _ := json.Marshal(map[string]any{"certificate_path": filepath.Join(m.root, names[0]+".pem")})
	os.WriteFile(filepath.Join(m.config, "sing-box.candidate.json"), b, 0600)
	if _, err := m.clean(11, digest); err == nil {
		t.Fatal("stale preview accepted")
	}
	os.Remove(filepath.Join(m.config, "sing-box.candidate.json"))
	os.WriteFile(filepath.Join(m.root, names[1]+".key"), []byte("modified"), 0600)
	if _, err := m.clean(12, digest); err == nil {
		t.Fatal("modified key accepted")
	}
	if _, err := os.Stat(filepath.Join(m.root, names[0]+".pem")); err != nil {
		t.Fatal("partial deletion before all-file preflight")
	}
	r, _ := m.load()
	r.Versions["../outside"] = r.Versions[names[0]]
	m.save(r)
	if _, err := m.inspect(); err == nil {
		t.Fatal("path escape accepted")
	}
}
func TestCertificateHistoryResumesPreparedDeletion(t *testing.T) {
	m, names := historyFixture(t)
	digest := historyDigest(t, m)
	r, _ := m.load()
	r.Jobs[20] = certificateCleanJob{Digest: digest, Versions: names[:2]}
	r.LastTask = 20
	if err := m.save(r); err != nil {
		t.Fatal(err)
	}
	// Equivalent to a crash after the first unlink, before registry commit.
	os.Remove(filepath.Join(m.root, names[0]+".pem"))
	if err := m.recover(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.root, names[1]+".key")); !os.IsNotExist(err) {
		t.Fatal("prepared cleanup not resumed")
	}
	if _, err := m.inspect(); err != nil {
		t.Fatal("recovered registry unusable", err)
	}
}

func TestCertificateHistoryRejectsSymlink(t *testing.T) {
	m, names := historyFixture(t)
	digest := historyDigest(t, m)
	outside := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(m.root, names[0]+".key")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(m.root, names[0]+".key")); err != nil {
		t.Skip("symlink creation unavailable; mandatory Linux suite covers this case")
	}
	if _, err := m.clean(30, digest); err == nil {
		t.Fatal("symlink accepted")
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != "untouched" {
		t.Fatal("outside file modified")
	}
}
