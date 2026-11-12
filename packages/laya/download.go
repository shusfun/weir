package laya

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Download publishes only an exact-sized, verified artifact. Hosts may replace
// transport with a relay; InstallModel always verifies the result again.
func Download(ctx context.Context, input DownloadRequest) error {
	if input.Size <= 0 {
		return errors.New("invalid artifact size")
	}
	if b, err := hex.DecodeString(input.SHA256); err != nil || len(b) != sha256.Size {
		return errors.New("invalid artifact digest")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return errors.New("invalid artifact URL")
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("artifact transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("artifact HTTP %d", resp.StatusCode)
	}
	if err = os.MkdirAll(filepath.Dir(input.Destination), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(input.Destination), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, input.Size+1))
	closeErr := f.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if copyErr != nil {
		return errors.New("artifact download interrupted")
	}
	if closeErr != nil {
		return closeErr
	}
	if n != input.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), input.SHA256) {
		return errors.New("artifact digest or size mismatch")
	}
	return os.Rename(f.Name(), input.Destination)
}
