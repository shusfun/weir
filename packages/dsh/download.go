package dsh

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
)

type Progress struct {
	Done, Total int64
	Speed       float64
}
type DownloadRequest struct {
	URLs         []string
	Dest, SHA256 string
	MaxSize      int64
	HTTP         *http.Client
	OnProgress   func(Progress)
	OnStep       func(string)
	Log          func(string)
}
type DownloadFunc func(context.Context, DownloadRequest) error

func (in Installer) download(ctx context.Context, req DownloadRequest) (err error) {
	end := observe(ctx, "dsh.download", in.Observe)
	defer func() { end(err) }()
	if in.Download != nil {
		return in.Download(ctx, req)
	}
	return Download(ctx, req)
}

// Download tries configured sources and validates the official digest before publishing.
func Download(ctx context.Context, req DownloadRequest) error {
	want, err := hex.DecodeString(req.SHA256)
	if err != nil || len(want) != sha256.Size || req.MaxSize <= 0 {
		return errors.New("invalid download manifest")
	}
	if err := os.MkdirAll(filepath.Dir(req.Dest), 0o700); err != nil {
		return err
	}
	client := req.HTTP
	if client == nil {
		client = downloadClient()
	}
	var last error = errors.New("no download sources")
	for _, raw := range req.URLs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if req.OnStep != nil {
			req.OnStep("download")
		}
		last = downloadOne(ctx, client, raw, req)
		if last == nil {
			return nil
		}
	}
	return last
}

func downloadOne(ctx context.Context, client *http.Client, raw string, req DownloadRequest) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return errors.New("invalid download URL")
	}
	resp, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("download transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > req.MaxSize {
		return errors.New("download exceeds size limit")
	}
	f, err := os.CreateTemp(filepath.Dir(req.Dest), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, req.MaxSize+1))
	closeErr := f.Close()
	if err != nil {
		return errors.New("download interrupted")
	}
	if closeErr != nil {
		return closeErr
	}
	if n > req.MaxSize {
		return errors.New("download exceeds size limit")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), req.SHA256) {
		return errors.New("download digest mismatch")
	}
	if req.OnProgress != nil {
		req.OnProgress(Progress{Done: n, Total: n})
	}
	return os.Rename(f.Name(), req.Dest)
}
