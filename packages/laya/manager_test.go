package laya

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRequiresExactContentAddressedArtifactSet(t *testing.T) {
	model := FixedManifest()
	if err := validateModelManifest(model); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Model){
		func(m *Model) { m.Files = m.Files[1:] },
		func(m *Model) { m.Files[0].Name = "mutable-file" },
		func(m *Model) { m.Files[0].Size++ },
		func(m *Model) { m.Files = append(m.Files, artifact("unreferenced", []byte("x"))) },
		func(m *Model) { m.ManifestPath = "blobs/manifest" },
	} {
		bad := FixedManifest()
		change(&bad)
		if err := validateModelManifest(bad); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}

func TestManifestActivationAndRollbackPreserveImmutableBlobs(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{}
	makeModel := func(content string) Model {
		file := artifact("", []byte(content))
		file.Name = "blobs/sha256-" + file.SHA256
		files[file.Name] = []byte(content)
		config := artifact("", []byte("config"))
		config.Name = "blobs/sha256-" + config.SHA256
		files[config.Name] = []byte("config")
		raw, _ := json.Marshal(map[string]any{"schemaVersion": 2, "config": modelDescriptor{Digest: "sha256:" + config.SHA256, Size: config.Size}, "layers": []modelDescriptor{{Digest: "sha256:" + file.SHA256, Size: file.Size}}})
		return Model{Files: []File{config, file}, Manifest: raw, ManifestPath: "manifests/test/model/v1"}
	}
	one, two := makeModel("first"), makeModel("second")
	in := Installer{Download: func(_ context.Context, r DownloadRequest) error {
		return os.WriteFile(r.Destination, files["blobs/sha256-"+r.SHA256], 0600)
	}}
	for _, model := range []Model{one, two, one} {
		if err := in.InstallModel(t.Context(), dir, model); err != nil {
			t.Fatal(err)
		}
		active, err := os.ReadFile(filepath.Join(dir, model.ManifestPath))
		if err != nil || string(active) != string(model.Manifest) {
			t.Fatalf("active=%s err=%v", active, err)
		}
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != string(want) {
			t.Fatalf("old blob lost: %s %v", name, err)
		}
	}
}

func artifact(name string, data []byte) File {
	sum := sha256.Sum256(data)
	return File{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), URL: "https://example.com/pinned/artifact"}
}

func TestInstallerVerifiesWholeSetBeforePublishingAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "first"), []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	model := Model{Files: []File{artifact("first", []byte("good")), artifact("second", []byte("expected"))}}
	var events []Event
	in := Installer{Download: func(ctx context.Context, r DownloadRequest) error {
		if filepath.Base(r.Destination) == "first" {
			return os.WriteFile(r.Destination, []byte("good"), 0600)
		}
		return os.WriteFile(r.Destination, []byte("tampered"), 0600)
	}, Observer: func(_ context.Context, e Event) { events = append(events, e) }}
	if err := in.InstallModel(context.Background(), dir, model); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	if old, _ := os.ReadFile(filepath.Join(dir, "first")); string(old) != "previous" {
		t.Fatalf("modified previous model: %s", old)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging not cleaned: %v %v", entries, err)
	}
	if len(events) != 1 || events[0].Status != "failed" {
		t.Fatalf("events=%+v", events)
	}
}

func TestInstallerCancellationResourcesAndUnsafePaths(t *testing.T) {
	called := false
	in := Installer{Download: func(context.Context, DownloadRequest) error { called = true; return nil }}
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", "a\\..\\escape", "C:/escape", "."} {
		if err := in.InstallModel(context.Background(), t.TempDir(), Model{Files: []File{artifact(name, []byte("data"))}}); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := Model{MinimumMemory: 1024, Files: []File{artifact("file", []byte("data"))}}
	if err := in.InstallModel(ctx, t.TempDir(), model); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	in.Resources = func(context.Context) (Resources, error) {
		return Resources{AvailableDisk: 100, AvailableMemory: 1}, nil
	}
	if err := in.InstallModel(context.Background(), t.TempDir(), model); err == nil {
		t.Fatal("insufficient memory accepted")
	}
	if called {
		t.Fatal("download ran before validation")
	}
}

func TestInstallerRejectsSymlinkEscape(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	in := Installer{Download: func(_ context.Context, r DownloadRequest) error {
		return os.WriteFile(r.Destination, []byte("ok"), 0600)
	}}
	err := in.InstallModel(context.Background(), dir, Model{Files: []File{artifact("escape/file", []byte("ok"))}})
	if err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatalf("wrote outside model root: %v", err)
	}
}
