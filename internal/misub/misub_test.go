package misub

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type memCache struct {
	mu sync.Mutex
	m  map[int][]string
}

func (c *memCache) Load(_ context.Context, id int) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[id]
	return v, ok
}

func (c *memCache) Store(_ context.Context, id int, nodes []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[id] = append([]string(nil), nodes...)
}

func trojan(i int, name string) string {
	return fmt.Sprintf("trojan://pass%d@198.51.100.%d:443?sni=example.com#%s", i, i, name)
}

func TestCombineSourcesManualFilterCacheAndRender(t *testing.T) {
	var mu sync.Mutex
	count := 12
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n := count
		mu.Unlock()
		var lines []string
		for i := 1; i <= n; i++ {
			lines = append(lines, trojan(i, fmt.Sprintf("HK-%02d", i)))
		}
		lines = append(lines, trojan(99, "过期时间"))
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=100; expire=1999999999")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))))
	}))
	defer srv.Close()
	f := Fetcher{AllowPrivate: true}
	cache := &memCache{m: map[int][]string{}}
	src := Source{ID: 1, Name: "机场A", URL: srv.URL, FilterExclude: "过期", NodeCache: true}
	manual := []ManualNode{{ID: 2, Name: "家宽", URL: "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pw")) + "@203.0.113.9:8388#x"}}
	opts := Options{UserAgent: "clash-meta/1.19", PrependManual: true, PrependSources: true}

	list, res := Combine(context.Background(), f, cache, []Source{src}, manual, opts)
	lines := strings.Split(strings.TrimSpace(list), "\n")
	if len(lines) != 13 || res[0].Nodes != 12 || res[0].UserInfo == "" || strings.Contains(list, "%E8%BF%87%E6%9C%9F") {
		t.Fatalf("combined %d lines %+v\n%s", len(lines), res, list)
	}
	if !strings.Contains(lines[0], "%E6%89%8B%E5%8A%A8%E8%8A%82%E7%82%B9") || !strings.Contains(list, "%E6%9C%BA%E5%9C%BAA") {
		t.Fatalf("prefixes missing:\n%s", list)
	}
	// 上游骤减到 2 个：保护性缓存保留上次的 12 个。
	mu.Lock()
	count = 2
	mu.Unlock()
	src.KnownNodes = 12
	list2, res2 := Combine(context.Background(), f, cache, []Source{src}, nil, opts)
	if n := len(strings.Split(strings.TrimSpace(list2), "\n")); n != 12 || !res2[0].FromCache || res2[0].Error != "node_count_drop_rejected" {
		t.Fatalf("protective cache: %d nodes %+v", n, res2)
	}
	// 私网地址默认拒绝。
	if _, err := (Fetcher{}).Get(context.Background(), srv.URL, "ua"); err == nil {
		t.Fatal("loopback source must be rejected")
	}
	if _, err := (Fetcher{}).Get(context.Background(), "http://169.254.169.254/latest", "ua"); err == nil {
		t.Fatal("metadata address must be rejected")
	}

	for target, want := range map[string]string{"clash": "proxy-groups:", "singbox": `"outbounds"`, "surge": "[Proxy]", "base64": ""} {
		body, ct, err := Render(list, target, RenderOptions{FileName: "测试组", UserAgent: "clash-meta"}, Template{})
		if err != nil || len(body) == 0 || ct == "" || !strings.Contains(string(body), want) {
			t.Fatalf("%s: err=%v ct=%s body=%.200s", target, err, ct, body)
		}
	}
	tpl, ok := BuiltinTemplate("clash_acl4ssr_lite")
	if !ok || !tpl.INI {
		t.Fatalf("builtin template = %+v", tpl)
	}
	body, _, err := Render(list, "clash", RenderOptions{FileName: "测试组", UserAgent: "clash-meta"}, tpl)
	if err != nil || !strings.Contains(string(body), "rules:") {
		t.Fatalf("ini template render: %v %.300s", err, body)
	}
	if Target("clash.meta/1.19", "") != "clash" || Target("v2rayN", "target=singbox") != "singbox" {
		t.Fatal("target detection")
	}
}

func TestLegacyTransformAndNotice(t *testing.T) {
	ops := legacyOperators(map[string]any{"enabled": true, "dedup": map[string]any{"enabled": true, "mode": "serverPort"}, "sort": map[string]any{"enabled": true, "keys": []any{map[string]any{"key": "name"}}}})
	if len(ops) != 2 {
		t.Fatalf("ops = %v", ops)
	}
	if !strings.Contains(NoticeNode("流量剩余 ≫ 1 GB"), "@127.0.0.1:443#") || isRealProxyNode(NoticeNode("x")) {
		t.Fatal("notice node")
	}
	if FormatBytes(1536) != "1.5 KB" || FormatBytes(0) != "0 B" {
		t.Fatal(FormatBytes(1536))
	}
}
