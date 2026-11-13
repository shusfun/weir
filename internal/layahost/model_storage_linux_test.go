//go:build linux

package layahost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shusfun/weir/packages/laya"
)

func TestModelStorageUsesApprovedVolumeFilesystem(t *testing.T) {
	dir := t.TempDir()
	const volume = "test-models"
	for _, tc := range []struct {
		name, driver, mount string
		options             map[string]string
		valid               bool
	}{
		{volume, "local", dir, nil, true},
		{"other", "local", dir, nil, false},
		{volume, "plugin", dir, nil, false},
		{volume, "local", dir, map[string]string{"device": "/"}, false},
		{volume, "local", "relative", nil, false},
	} {
		execute := func(_ context.Context, args []string) ([]byte, error) {
			if !reflect.DeepEqual(args, []string{"volume", "inspect", volume}) {
				t.Fatalf("unexpected command: %v", args)
			}
			return json.Marshal([]map[string]any{{"Name": tc.name, "Driver": tc.driver, "Mountpoint": tc.mount, "Options": tc.options}})
		}
		storage, err := inspectModelStorage(t.Context(), execute, volume)
		if !tc.valid {
			if ErrorCode(err) != "laya_resources_unknown" || storage != nil {
				t.Fatalf("unsafe volume accepted: %+v %v", storage, err)
			}
			continue
		}
		var size uint64 = 1 << 20
		for _, f := range laya.FixedManifest().Files {
			size += uint64(f.Size)
		}
		if err != nil || storage.RequiredBytes != size || storage.AvailableBytes == 0 {
			t.Fatalf("storage: %+v %v", storage, err)
		}
	}
}

func TestModelStorageDoesNotCreditEscapingSymlinks(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "models")); err != nil {
		t.Fatal(err)
	}
	if _, err := missingModelBytes(t.Context(), dir, laya.FixedManifest()); err == nil {
		t.Fatal("escaped approved volume")
	}
}
