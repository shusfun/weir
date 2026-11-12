package gate

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/releasefiles"
	"github.com/shusfun/weir/packages/contracts"
)

func kernelArchiveFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	body := make([]byte, 120)
	copy(body, []byte{'\x7f', 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(body[16:], 2)
	machine := uint16(62)
	if runtime.GOARCH == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(body[18:], machine)
	binary.LittleEndian.PutUint32(body[20:], 1)
	binary.LittleEndian.PutUint64(body[32:], 64)
	binary.LittleEndian.PutUint16(body[52:], 64)
	binary.LittleEndian.PutUint16(body[54:], 56)
	binary.LittleEndian.PutUint16(body[56:], 1)
	binary.LittleEndian.PutUint32(body[64:], 1)
	binary.LittleEndian.PutUint32(body[68:], 5)
	binary.LittleEndian.PutUint64(body[96:], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[104:], uint64(len(body)))
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	member, err := writer.Create("xray")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = member.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), body
}

func TestKernelDownloadDirectThenAuthenticatedPinnedFallback(t *testing.T) {
	archive, expected := kernelArchiveFixture(t)
	for _, direct := range []bool{true, false} {
		t.Run(map[bool]string{true: "direct", false: "fallback"}[direct], func(t *testing.T) {
			var directHits, fallbackHits atomic.Int32
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				directHits.Add(1)
				if !direct {
					w.WriteHeader(502)
					return
				}
				http.ServeContent(w, r, "xray.zip", time.Time{}, bytes.NewReader(archive))
			}))
			t.Cleanup(upstream.Close)
			trace := strings.Repeat("a", 32)
			a := testAgent(t, upstream, func(w http.ResponseWriter, r *http.Request) {
				fallbackHits.Add(1)
				if r.URL.Path != "/api/v1/agent/kernel-assets/xray/26.3.27" || r.Header.Get("Authorization") != "Bearer t" || !strings.Contains(r.Header.Get("traceparent"), trace) {
					t.Error("fallback identity or trace missing", r.URL.Path)
					w.WriteHeader(403)
					return
				}
				http.ServeContent(w, r, "xray.zip", time.Time{}, bytes.NewReader(archive))
			})
			artifact := contracts.KernelArtifact{Schema: 1, Kernel: "xray", Version: "26.3.27", Arch: runtime.GOARCH, URLs: []string{upstream.URL + "/XTLS/Xray-core/releases/download/v26.3.27/Xray.zip"}, SHA256: sumOf(archive), Size: int64(len(archive))}
			ctx := releasefiles.WithTrace(t.Context(), "00-"+trace+"-1111111111111111-01")
			got, err := a.downloadKernel(ctx, "xray", contracts.Kernel{Version: artifact.Version, Artifact: &artifact})
			if err != nil || !bytes.Equal(got, expected) {
				t.Fatalf("download: %v", err)
			}
			if directHits.Load() == 0 || direct && fallbackHits.Load() != 0 || !direct && fallbackHits.Load() == 0 {
				t.Fatal("wrong download path")
			}
			if _, err := os.Stat(filepath.Join(a.P.Data, "kernel-downloads", artifact.SHA256)); !os.IsNotExist(err) {
				t.Fatal("archive retained after extraction")
			}
		})
	}
}

func TestCenterKernelFallbackResumesWithRangeAndRejectsChangedBytes(t *testing.T) {
	body := []byte("verified kernel archive content")
	var ranges atomic.Int32
	a := testAgent(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=8-" {
			ranges.Add(1)
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Error("missing bearer")
		}
		http.ServeContent(w, r, "archive", time.Time{}, bytes.NewReader(body))
	})
	dest := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(dest+".part", body[:8], 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.center.downloadFile(t.Context(), "/agent/kernel-assets/xray/1.0.0", sumOf(body), int64(len(body)), dest); err != nil {
		t.Fatal(err)
	}
	if ranges.Load() != 1 {
		t.Fatal("fallback did not resume")
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("wrong resumed content", err)
	}
	if err := a.center.downloadFile(t.Context(), "/agent/kernel-assets/xray/1.0.0", strings.Repeat("b", 64), int64(len(body)), dest+"-bad"); err == nil {
		t.Fatal("wrong digest accepted")
	}
}
