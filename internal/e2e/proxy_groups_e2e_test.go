//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/subout"
	"github.com/shusfun/weir/packages/contracts"
	"gopkg.in/yaml.v3"
)

func seedSubscriptionGeoIP(t *testing.T, dir string) {
	t.Helper()
	source := os.Getenv("WEIR_E2E_GEOIP")
	if source == "" {
		return
	}
	b, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "Country.mmdb"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProxyGroupsRealKernels(t *testing.T) {
	if os.Getenv("WEIR_E2E_GEOIP") == "" {
		t.Fatal("需要有效的 GeoIP 缓存，不能删除国内规则")
	}
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "primary") }))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "secondary") }))
	defer secondary.Close()
	a, _ := socksServer(t, strings.TrimPrefix(primary.URL, "http://"))
	b, _ := socksServer(t, strings.TrimPrefix(secondary.URL, "http://"))
	items := []subout.Item{{Link: fmt.Sprintf("socks5://127.0.0.1:%d#Primary", a), Ref: "node:1", Auto: true, Direct: true}, {Link: fmt.Sprintf("socks5://127.0.0.1:%d#Secondary", b), Ref: "chain:2", Auto: true}}
	plan := &subout.Plan{Groups: []contracts.ProxyGroup{{ID: 1, Name: "主备", Kind: "fallback", Members: []string{"node:1", "chain:2"}, IntervalSeconds: 30}}, Rules: []subout.Rule{{Kind: "geoip", Values: []string{"cn"}, Target: "direct"}}, TestURL: "http://probe.example.test/"}
	items = append(items, subout.Item{Link: fmt.Sprintf("socks5://127.0.0.1:%d#美东", a), Ref: "node:3"}, subout.Item{Link: fmt.Sprintf("socks5://127.0.0.1:%d#美东", b), Ref: "node:6"})
	plan.Groups = append(plan.Groups, contracts.ProxyGroup{ID: 2, Name: "手动", Kind: "select", Members: []string{"node:3", "node:6"}})
	plan.Groups = append(plan.Groups, contracts.ProxyGroup{ID: 3, Name: "链式", Kind: "url-test", Members: []string{"node:3", "node:6"}, IntervalSeconds: 60, ToleranceMS: 50})

	for _, target := range []string{"clash", "singbox"} {
		body, _, err := subout.RenderPlan(items, target, plan)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		seedSubscriptionGeoIP(t, dir)
		file := filepath.Join(dir, "config.json")
		os.WriteFile(file, body, 0600)
		bin := os.Getenv("WEIR_E2E_SINGBOX")
		args := []string{"check", "-c", file}
		if target == "clash" {
			bin = os.Getenv("WEIR_E2E_MIHOMO")
			args = []string{"-t", "-d", dir, "-f", file}
		}
		if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s 配置检查：%v %s", target, err, out)
		}
	}
	var mu sync.Mutex
	providers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, err := subout.ClashProvider(items, plan, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		w.Write(body)
	}))
	defer providers.Close()
	plan.Providers = func(key string) string { return providers.URL + "/" + key }
	body, _, err := subout.RenderPlan(items, "clash", plan)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seedSubscriptionGeoIP(t, dir)
	controller := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	cfg["external-controller"], cfg["mixed-port"], cfg["log-level"] = controller, freePort(t), "warning"
	// 只缩短内核的探测节拍，生成器本身仍受 30 秒下限约束。
	for _, raw := range cfg["proxy-groups"].([]any) {
		g := raw.(map[string]any)
		if g["type"] == "fallback" {
			g["interval"] = 1
		}
	}
	cfg["proxy-providers"].(map[string]any)["p-group-1"].(map[string]any)["health-check"].(map[string]any)["interval"] = 1
	body, _ = json.Marshal(cfg)
	file := filepath.Join(dir, "config.json")
	os.WriteFile(file, body, 0600)
	cmd := exec.Command(os.Getenv("WEIR_E2E_MIHOMO"), "-d", dir, "-f", file)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := &http.Client{Timeout: 2 * time.Second}
	get := func(path string) map[string]any {
		resp, err := client.Get("http://" + controller + path)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	eventually(t, 15*time.Second, "PROXY 默认与顺序", func() bool {
		g := get("/proxies/" + url.PathEscape("PROXY"))
		return g != nil && g["now"] == "自动选择" && reflect.DeepEqual(g["all"], []any{"自动选择", "主备", "手动", "链式"})
	})
	eventually(t, 15*time.Second, "主备订阅成员加载", func() bool {
		return reflect.DeepEqual(get("/proxies/" + url.PathEscape("主备"))["all"], []any{"主备 · 主备切换", "Primary", "Secondary"})
	})
	if g := get("/proxies/" + url.PathEscape("主备")); g["hidden"] != false || g["now"] != "主备 · 主备切换" || !reflect.DeepEqual(g["all"], []any{"主备 · 主备切换", "Primary", "Secondary"}) {
		t.Fatalf("主备顺序=%v", g)
	}
	for _, name := range []string{"自动选择", "主备 · 主备切换", "链式 · 自动选择"} {
		if group := get("/proxies/" + url.PathEscape(name)); group["hidden"] != true {
			t.Fatalf("子组未隐藏：%s %v", name, group)
		}
	}
	manual := get("/proxies/" + url.PathEscape("手动"))
	members, ok := manual["all"].([]any)
	if !ok || len(members) != 2 || members[0] == members[1] || manual["hidden"] != false || manual["type"] != "Selector" {
		t.Fatalf("手动组不可选择或同名成员丢失：%v", manual)
	}
	choose := func(group, member string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"name": member})
		req, _ := http.NewRequest("PUT", "http://"+controller+"/proxies/"+url.PathEscape(group), strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("选择 %s：HTTP %d", group, resp.StatusCode)
		}
	}
	choose("PROXY", "手动")
	choose("手动", members[1].(string))
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg["mixed-port"].(int)))
	traffic := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	resp, err := traffic.Get("http://probe.example.test/")
	if err != nil {
		t.Fatal(err)
	}
	gotBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	traffic.CloseIdleConnections()
	if err != nil || string(gotBody) != "secondary" {
		t.Fatalf("手动节点未生效：%q %v", gotBody, err)
	}
	chain := get("/proxies/" + url.PathEscape("链式"))
	if chain["hidden"] != false || chain["type"] != "Selector" || chain["now"] != "链式 · 自动选择" || !reflect.DeepEqual(chain["all"], []any{"链式 · 自动选择", members[0], members[1]}) {
		t.Fatalf("自动组没有可见手动入口：%v", chain)
	}
	choose("PROXY", "链式")
	choose("链式", members[1].(string))
	resp, err = traffic.Get("http://probe.example.test/")
	if err != nil {
		t.Fatal(err)
	}
	gotBody, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(gotBody) != "secondary" {
		t.Fatalf("自动组中手动指定节点未生效：%q %v", gotBody, err)
	}
	choose("链式", "链式 · 自动选择")
	if g := get("/proxies/" + url.PathEscape("链式")); g["now"] != "链式 · 自动选择" {
		t.Fatalf("不能恢复自动选择：%v", g)
	}
	resp, err = traffic.Get("http://probe.example.test/")
	if err != nil {
		t.Fatal(err)
	}
	gotBody, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || (string(gotBody) != "primary" && string(gotBody) != "secondary") {
		t.Fatalf("恢复自动后无有效流量：%q %v", gotBody, err)
	}
	choose("PROXY", "自动选择")
	eventually(t, 10*time.Second, "主用可用", func() bool { return get("/proxies/" + url.PathEscape("主备 · 主备切换"))["now"] == "Primary" })
	primary.Close()
	// 触发对应集合健康检查并核对内核实际切到备用。
	resp, err = client.Get("http://" + controller + "/providers/proxies/p-group-1/healthcheck")
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	eventually(t, 15*time.Second, "故障切换备用", func() bool { return get("/proxies/" + url.PathEscape("主备 · 主备切换"))["now"] == "Secondary" })
	choose("PROXY", "主备")
	resp, err = traffic.Get("http://probe.example.test/")
	if err != nil {
		t.Fatal(err)
	}
	gotBody, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(gotBody) != "secondary" {
		t.Fatalf("主备入口未保留自动切换流量：%q %v", gotBody, err)
	}
	mu.Lock()
	plan.Groups[0].Members = []string{"chain:2", "node:1"}
	mu.Unlock()
	req, _ := http.NewRequest("PUT", "http://"+controller+"/providers/proxies/p-group-1", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	eventually(t, 10*time.Second, "集合刷新后成员重排", func() bool {
		return reflect.DeepEqual(get("/proxies/" + url.PathEscape("主备"))["all"], []any{"主备 · 主备切换", "Secondary", "Primary"}) && reflect.DeepEqual(get("/proxies/" + url.PathEscape("主备 · 主备切换"))["all"], []any{"Secondary", "Primary"})
	})
	t.Log("真实内核：配置加载、PROXY 默认、成员顺序、provider 刷新和故障转移通过")
}
