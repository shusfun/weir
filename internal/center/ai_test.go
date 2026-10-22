package center

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

// fakeGate 处理 node.test 动作：前 n 次返回失败，之后成功；记录收到的测试配置。
func (g *fakeGate) serveNodeTests(t *testing.T, fail int, seen *[]map[string]any, stop chan struct{}) {
	go func() {
		count := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			code, body := g.call("GET", "/api/v1/agent/actions", nil)
			if code != 200 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			var acts []contracts.Action
			json.Unmarshal(body, &acts)
			for _, a := range acts {
				res := contracts.ActionResult{OK: true, Result: map[string]any{"stage": "connect", "latency_ms": 3}}
				if a.Kind == contracts.ActionNodeTest {
					*seen = append(*seen, a.Payload)
					if count < fail {
						res = contracts.ActionResult{Error: "经候选配置连接失败：EOF", Result: map[string]any{"stage": "connect"}}
					}
					count++
				}
				g.call("POST", "/api/v1/agent/actions/"+itoa(int(a.ID)), res)
			}
		}
	}()
}

func TestAIDraftLoopNeverLeaksSecretsAndNeedsPassingTest(t *testing.T) {
	s, admin, g := setupGate(t)
	node := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": g.id, "preset": "vless-reality", "skip_probe": true}, 200)
	nid := int(node["id"].(float64))
	priv := node["settings"].(map[string]any)["reality_private_key"].(string)

	// 假模型：第 1 轮试图改私钥（系统拒绝），第 2 轮合法但 Gate 实测失败，第 3 轮通过。
	var mu sync.Mutex
	var prompts []string
	round := 0
	answers := []string{
		`{"set":{"reality_private_key":"stolen"},"explain":"换密钥"}`,
		`{"set":{"reality_server_names":"www.apple.com","reality_dest":"www.apple.com:443"},"explain":"换伪装目标"}`,
		"```json\n{\"set\":{\"reality_server_names\":\"www.amazon.com\",\"reality_dest\":\"www.amazon.com:443\"},\"explain\":\"改用 amazon\"}\n```",
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		prompts = append(prompts, string(b))
		a := answers[round%len(answers)]
		round++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": a}}}})
	}))
	defer model.Close()

	mustJSON(t, admin, "PUT", "/api/v1/admin/ai/provider", map[string]any{"protocol": "openai", "base_url": model.URL, "model": "m", "key": "sk-test-key", "password": "wrong"}, 401)
	view := mustJSON(t, admin, "PUT", "/api/v1/admin/ai/provider", map[string]any{"protocol": "openai", "base_url": model.URL, "model": "m", "key": "sk-test-key", "password": "Passw0rd"}, 200)
	if strings.Contains(mustString(view), "sk-test-key") || view["key_set"] != true {
		t.Fatalf("provider view leaks key: %v", view)
	}

	var seen []map[string]any
	stop := make(chan struct{})
	defer func() {
		close(stop)
		// 唤醒假 Gate 挂起的动作长轮询，让它看到停止信号。
		s.enqueueAction(t.Context(), int(g.id), contracts.ActionDiagnostics, nil, time.Second)
	}()
	g.serveNodeTests(t, 1, &seen, stop)

	d := mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts", map[string]any{"node_id": nid, "instruction": "伪装目标换成更稳定的站点"}, 202)
	id := d["id"].(string)
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 409)
	var final map[string]any
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		final = mustJSON(t, admin, "GET", "/api/v1/admin/ai/drafts/"+id, nil, 200)
		if final["status"] != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if final["status"] != "passed" || len(final["attempts"].([]any)) != 3 || len(seen) != 2 {
		t.Fatalf("draft = %v (gate tests %d)", final, len(seen))
	}
	mu.Lock()
	all := strings.Join(prompts, "\n")
	mu.Unlock()
	// 请求体是 JSON，"<" 会被转义，只查占位符的文字部分。
	if strings.Contains(all, priv) || !strings.Contains(all, "redacted") || !strings.Contains(all, "上一份草稿未通过") {
		t.Fatalf("model prompts must be redacted and carry the failure feedback: leak=%v feedback=%v", strings.Contains(all, priv), strings.Contains(all, "上一份草稿未通过"))
	}
	if b, _ := json.Marshal(final); strings.Contains(string(b), priv) {
		t.Fatal("draft view leaks the private key")
	}
	// Gate 实测用的是测试凭据，不是任何用户凭据；客户端出站由订阅渲染得到。
	if cfg, _ := seen[1]["config"].(map[string]any); cfg == nil || seen[1]["client"] == nil {
		t.Fatalf("node test payload = %v", seen[1])
	}
	applied := mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 200)
	st := applied["settings"].(map[string]any)
	if st["reality_server_names"] != "www.amazon.com" || st["reality_private_key"] != priv {
		t.Fatalf("applied settings = %v", st)
	}
	n, _ := s.Store.Ent.Node.Get(t.Context(), nid)
	if n.Settings["reality_dest"] != "www.amazon.com:443" {
		t.Fatalf("node not updated: %v", n.Settings)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+id+"/apply", nil, 404)
}

func mustString(v any) string { b, _ := json.Marshal(v); return string(b) }
