package center

import (
	"strings"
	"testing"
)

func TestSubscriptionDeliveryRevision(t *testing.T) {
	s, admin, gate := setupGate(t)
	n := mustJSON(t, admin, "POST", "/api/v1/admin/nodes", map[string]any{"gate_id": gate.id, "preset": "ss2022", "skip_probe": true}, 200)
	links := mustJSON(t, admin, "GET", "/api/v1/me/links", nil, 200)["items"].([]any)
	token := links[0].(map[string]any)["token"].(string)
	view := mustJSON(t, admin, "GET", "/api/v1/proxy", nil, 200)
	revision := int(view["policy"].(map[string]any)["revision"].(float64))
	buf := captureLog(t)
	trace := "1234567890abcdef1234567890abcdef"
	headers := map[string]string{"traceparent": "00-" + trace + "-1111111111111111-01"}
	w := admin.do("POST", "/api/v1/proxy/groups", map[string]any{"revision": revision, "name": "探测组", "members": []string{"node:" + itoa(int(n["id"].(float64)))}}, headers)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	created := decode(t, w)
	groupID := int(created["id"].(float64))
	policy := created["policy"].(map[string]any)
	if int(policy["groups"].([]any)[0].(map[string]any)["interval_seconds"].(float64)) != 30 {
		t.Fatal("new groups must default to 30 seconds")
	}
	want := itoa(revision + 1)
	for _, suffix := range []string{"?target=clash", "/provider/auto", "/provider/group-" + itoa(groupID)} {
		w = admin.do("GET", "/sub/"+token+suffix, nil, headers)
		if w.Code != 200 || w.Header().Get("X-Weir-Policy-Revision") != want || w.Header().Get("X-Trace-Id") != trace {
			t.Fatalf("delivery metadata: %d %v", w.Code, w.Header())
		}
	}
	w = admin.do("GET", "/sub/"+token+"/provider/group-999999", nil, headers)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = admin.do("GET", "/sub/invalid-token/provider/auto", nil, headers)
	if w.Code != 404 || w.Header().Get("X-Weir-Policy-Revision") != "" {
		t.Fatal("unauthorized revision disclosure")
	}
	text := buf.String()
	for _, value := range []string{"subscription delivery", "proxy group saved", "policy_revision=" + want, "provider=group-" + itoa(groupID), "member_count=1", "result=failed", "trace=" + trace} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing delivery evidence %q", value)
		}
	}
	if strings.Contains(text, token) {
		t.Fatal("subscription token leaked into logs")
	}
	_ = s
}
