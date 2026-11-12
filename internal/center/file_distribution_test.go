package center

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/store/ent"
)

func TestCenterFileDistributionServesRangeAndRecordsOnlyMetadata(t *testing.T) {
	s, _ := newInstalledServer(t)
	body := []byte("center fallback verified bytes")
	path := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(requestLog)
	router.GET("/asset", func(c *gin.Context) {
		c.Set("gate", &ent.Gate{ID: 123})
		s.distributeFile(c, path)
	})
	trace := strings.Repeat("d", 32)
	r := httptest.NewRequest(http.MethodGet, "/asset", nil)
	r.Header.Set("Range", "bytes=7-14")
	r.Header.Set("traceparent", "00-"+trace+"-1111111111111111-01")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), body[7:15]) || w.Header().Get("Content-Range") == "" {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	rows, err := s.Store.Ent.Audit.Query().All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Action != "asset.distribute" {
			continue
		}
		found = true
		if row.Result != "ok" || row.Target != "123" || row.Detail["trace_id"] != trace || len(row.Detail) != 6 {
			t.Fatalf("distribution record: %+v", row)
		}
	}
	if !found {
		t.Fatal("distribution record missing")
	}
}

func TestKernelArchiveCacheReservesSpaceAndPrunesExpiredPartials(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old, recent, active := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	for _, name := range []string{old, recent, active + ".part"} {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		size := int64(150 << 20)
		if name == active+".part" {
			size = 8
		}
		if err = file.Truncate(size); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, old), now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := pruneKernelArchiveDir(t.Context(), dir, 200<<20, active, now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{old, recent} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("cache not bounded", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, active+".part")); err != nil {
		t.Fatal("active resumable partial removed", err)
	}
	if err := os.Chtimes(filepath.Join(dir, active+".part"), now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := pruneKernelArchiveDir(t.Context(), dir, 0, "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, active+".part")); !os.IsNotExist(err) {
		t.Fatal("expired partial retained")
	}
}
