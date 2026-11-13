package layahost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shusfun/weir/packages/laya"
)

func TestMissingModelBytesCreditsOnlyVerifiedBlobs(t *testing.T) {
	dir := t.TempDir()
	const content = "verified model blob"
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	file := laya.File{Name: "blobs/sha256-" + digest, Size: int64(len(content)), SHA256: digest}
	model := laya.Model{Files: []laya.File{file}}
	path := filepath.Join(dir, "models", file.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		want       uint64
	}{
		{"missing", "", uint64(file.Size)},
		{"partial", "verified", uint64(file.Size)},
		{"corrupt", "corrupted model xyz", uint64(file.Size)},
		{"complete", content, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := missingModelBytes(t.Context(), dir, model)
			if err != nil || got != tc.want+(1<<20) {
				t.Fatalf("got %d, err %v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := missingModelBytes(ctx, dir, model); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
}
