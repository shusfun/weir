package kernels

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		sign int
	}{{"26.3.27", "26.10.17", -1}, {"v1.14.3", "1.14.3", 0}, {"1.14.3-migate.1", "1.14.3", 0}, {"1.12.0", "1.9.9", 1}}
	for _, c := range cases {
		got := CompareVersions(c.a, c.b)
		if got < 0 && c.sign >= 0 || got > 0 && c.sign <= 0 || got == 0 && c.sign != 0 {
			t.Fatalf("Compare(%s,%s) = %d", c.a, c.b, got)
		}
	}
}

func TestProfileSelectionAndOverride(t *testing.T) {
	p := Profiles{}
	text, err := p.Select("xray", "26.10.17")
	if err != nil || !strings.Contains(text, "min_version: 26.3.27") {
		t.Fatalf("select new xray: %v\n%s", err, text)
	}
	if text, err = p.Select("xray", "25.1.30"); err != nil || !strings.Contains(text, "max_version: 26.3.26") {
		t.Fatalf("select old xray: %v\n%s", err, text)
	}
	if _, err = p.Select("sing-box", "1.10.0"); err == nil {
		t.Fatal("version below every profile accepted")
	}
	dir := t.TempDir()
	override := "kernel: xray\nmin_version: 26.3.27\nprotocols: [vless]\nnotes: overridden\n"
	if err = os.WriteFile(filepath.Join(dir, "xray-26.3.yaml"), []byte(override), 0644); err != nil {
		t.Fatal(err)
	}
	if text, _ = (Profiles{OverrideDir: dir}).Select("xray", "26.10.17"); !strings.Contains(text, "overridden") {
		t.Fatalf("override not applied:\n%s", text)
	}
	if err = os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("kernel: nope\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = (Profiles{OverrideDir: dir}).Select("xray", "26.10.17"); err == nil {
		t.Fatal("invalid override profile accepted")
	}
}

func TestCacheRejectsTamperAndBadNames(t *testing.T) {
	c := Cache{Dir: t.TempDir()}
	sum, err := c.Put("xray", "26.3.27", "amd64", []byte("binary"))
	if err != nil {
		t.Fatal(err)
	}
	path, got, err := c.Get("xray", "26.3.27", "amd64")
	if err != nil || got != sum {
		t.Fatalf("get = %s %v", got, err)
	}
	if err = os.WriteFile(path, []byte("tampered"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Get("xray", "26.3.27", "amd64"); err == nil {
		t.Fatal("tampered binary returned")
	}
	for _, bad := range [][3]string{{"curl", "1", "amd64"}, {"xray", "../../etc", "amd64"}, {"xray", "1", "mips"}} {
		if _, err = c.Put(bad[0], bad[1], bad[2], []byte("x")); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}

func zipWith(name string, data []byte) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create(name)
	f.Write(data)
	w.Close()
	return buf.Bytes()
}

func tarGzWith(name string, data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: tar.TypeReg})
	tw.Write(data)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func gzWith(data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(data)
	gz.Close()
	return buf.Bytes()
}

func digestOf(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }

// fakeGitHub 同时扮演 API 与下载主机；tamper 为真时下载内容与 API 摘要不一致。
func fakeGitHub(t *testing.T, tamper bool) (*httptest.Server, *Fetcher) {
	t.Helper()
	assets := map[string][]byte{
		"/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip":                         zipWith("xray", []byte("xray-bin")),
		"/shusfun/weir/releases/download/sing-box-v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz": tarGzWith("sing-box-1.14.3-linux-amd64/sing-box", []byte("sb-bin")),
		"/MetaCubeX/mihomo/releases/download/v1.19.33/mihomo-linux-amd64-v1.19.33.gz":          gzWith([]byte("mihomo-bin")),
	}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := srv.URL
		releases := map[string][]map[string]any{
			"/repos/XTLS/Xray-core/releases":   {{"tag_name": "v26.3.27", "assets": []map[string]any{{"name": "Xray-linux-64.zip", "browser_download_url": base + "/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip", "digest": digestOf(assets["/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"])}}}, {"tag_name": "v27.0.0-pre", "prerelease": true}},
			"/repos/shusfun/weir/releases":    {{"tag_name": "v0.1.10"}, {"tag_name": "sing-box-v1.14.3", "assets": []map[string]any{{"name": "sing-box-1.14.3-linux-amd64.tar.gz", "browser_download_url": base + "/shusfun/weir/releases/download/sing-box-v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz", "digest": digestOf(assets["/shusfun/weir/releases/download/sing-box-v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz"])}}}},
			"/repos/MetaCubeX/mihomo/releases": {{"tag_name": "v1.19.33", "assets": []map[string]any{{"name": "mihomo-linux-amd64-v1.19.33.gz", "browser_download_url": base + "/MetaCubeX/mihomo/releases/download/v1.19.33/mihomo-linux-amd64-v1.19.33.gz", "digest": digestOf(assets["/MetaCubeX/mihomo/releases/download/v1.19.33/mihomo-linux-amd64-v1.19.33.gz"])}}}},
		}
		if list, ok := releases[r.URL.Path]; ok {
			json.NewEncoder(w).Encode(list)
			return
		}
		if data, ok := assets[r.URL.Path]; ok {
			if tamper {
				data = append([]byte("x"), data...)
			}
			w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return srv, &Fetcher{HTTP: srv.Client(), API: srv.URL, DownloadHost: u.Host, Cache: Cache{Dir: t.TempDir()}}
}

func TestFetchAllKernelsAndListReleases(t *testing.T) {
	_, f := fakeGitHub(t, false)
	ctx := context.Background()
	for kernel, version := range map[string]string{"xray": "26.3.27", "sing-box": "1.14.3", "mihomo": "1.19.33"} {
		if _, err := f.Fetch(ctx, kernel, version, "amd64"); err != nil {
			t.Fatalf("%s: %v", kernel, err)
		}
		path, _, err := f.Cache.Get(kernel, version, "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); !strings.HasSuffix(string(b), "-bin") {
			t.Fatalf("%s extracted %q", kernel, b)
		}
	}
	rels, err := f.Releases(ctx, "xray")
	if err != nil || len(rels) != 1 || rels[0].Version != "26.3.27" {
		t.Fatalf("xray releases (pre-release must be skipped) = %+v %v", rels, err)
	}
	if rels, _ = f.Releases(ctx, "sing-box"); len(rels) != 1 || rels[0].Version != "1.14.3" {
		t.Fatalf("sing-box releases must only use sing-box-v tags: %+v", rels)
	}
}

func TestFetchRejectsDigestMismatch(t *testing.T) {
	_, f := fakeGitHub(t, true)
	if _, err := f.Fetch(context.Background(), "xray", "26.3.27", "amd64"); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := f.Cache.Get("xray", "26.3.27", "amd64"); err == nil {
		t.Fatal("tampered download cached")
	}
}
