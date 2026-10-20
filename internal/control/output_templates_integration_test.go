package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/user"
)

func testOutputTemplateIntegration(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	account, err := s.Store.User.Query().Where(user.EmailEQ("alice@example.test")).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Store.Device.Create().SetName("template-fixture").SetPublicKey("template-fixture").SetPairExpires(time.Now()).SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	in, err := s.Store.Resource.Create().SetKind("device-inbound").SetKey(fmt.Sprintf("%d:99", d.ID)).SetData(map[string]any{"public_host": "127.0.0.1", "protocol": "trojan", "port": 24445, "security": "tls", "remark": "HK-owned", "tls_sni": "example.com"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(in).Exec(ctx)
	g, err := s.Store.Grant.Create().SetUserID(account.ID).SetDeviceID(d.ID).SetInboundID(99).SetStatus("applied").SetCredential(map[string]any{"password": "owned-test-password"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Grant.DeleteOne(g).Exec(ctx)
	response := request("POST", "/api/v1/admin/resources/templates", map[string]any{"data": templateFixture("singbox")}, "", admin)
	if response.Code != 200 {
		t.Fatalf("template save: %d", response.Code)
	}
	var saved struct{ ID int }
	json.Unmarshal(response.Body.Bytes(), &saved)
	defer s.Store.Resource.DeleteOneID(saved.ID).Exec(ctx)
	profile, err := s.Store.Resource.Create().SetKind("profiles").SetKey("template-profile-fixture").SetData(map[string]any{"user_ids": []any{account.ID}, "template_ids": []any{saved.ID}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(profile).Exec(ctx)
	response = request("GET", "/sub/"+account.SubscriptionToken+"?target=singbox", nil, "", "")
	if response.Code != 200 {
		t.Fatalf("assigned template output: %d %s", response.Code, response.Body.String())
	}
	var cfg map[string]any
	json.Unmarshal(response.Body.Bytes(), &cfg)
	out := cfg["outbounds"].([]any)
	if len(out) != 4 || out[2].(map[string]any)["password"] != "owned-test-password" || cfg["route"].(map[string]any)["final"] != "Choice" || strings.Contains(response.Body.String(), "8388") {
		t.Fatal("template lost authorization boundary")
	}
	if request("POST", "/api/v1/admin/resources/templates", map[string]any{"data": templateFixture("singbox")}, "", alice).Code != 403 {
		t.Fatal("user can write template")
	}
	bad := templateFixture("singbox")
	bad["proxies"] = []string{"outside-node"}
	if request("POST", "/api/v1/admin/resources/templates", map[string]any{"data": bad}, "", admin).Code != 400 {
		t.Fatal("template can inject credentials")
	}
	remote := remoteTemplateFixture("singbox")
	response = request("POST", "/api/v1/admin/resources/templates", map[string]any{"id": saved.ID, "data": remote}, "", admin)
	if response.Code != 200 {
		t.Fatalf("remote template save HTTP %d", response.Code)
	}
	response = request("GET", "/sub/"+account.SubscriptionToken+"?target=singbox", nil, "", "")
	if response.Code != 200 {
		t.Fatalf("remote subscription HTTP %d", response.Code)
	}
	json.Unmarshal(response.Body.Bytes(), &cfg)
	if len(cfg["outbounds"].([]any)) != 4 || cfg["outbounds"].([]any)[2].(map[string]any)["password"] != "owned-test-password" || len(cfg["route"].(map[string]any)["rule_set"].([]any)) != 1 {
		t.Fatal("remote rules changed authorized inventory")
	}
	remote["rule_sets"].([]any)[0].(map[string]any)["url"] = "https://user:secret@example.org/rules"
	if request("POST", "/api/v1/admin/resources/templates", map[string]any{"id": saved.ID, "data": remote}, "", admin).Code != 400 {
		t.Fatal("credential URL persisted")
	}
	auto := map[string]any{"name": "auto", "target": "singbox", "auto_groups": "protocol", "groups": []any{map[string]any{"name": "AutoChoice", "type": "select", "members": []string{"$auto"}}}, "rules": []any{map[string]any{"type": "FINAL", "policy": "AutoChoice"}}}
	if request("POST", "/api/v1/admin/resources/templates", map[string]any{"id": saved.ID, "data": auto}, "", admin).Code != 200 {
		t.Fatal("auto template save failed")
	}
	response = request("GET", "/sub/"+account.SubscriptionToken+"?target=singbox", nil, "", "")
	if response.Code != 200 {
		t.Fatalf("auto subscription HTTP %d", response.Code)
	}
	json.Unmarshal(response.Body.Bytes(), &cfg)
	autoOut := cfg["outbounds"].([]any)
	if len(autoOut) != 4 || autoOut[1].(map[string]any)["type"] != "urltest" || len(autoOut[1].(map[string]any)["outbounds"].([]any)) != 1 || autoOut[2].(map[string]any)["password"] != "owned-test-password" {
		t.Fatal("auto groups expanded outside user authorization")
	}
	other, err := s.Store.Resource.Create().SetKind("profiles").SetKey("template-ambiguous-fixture").SetData(profile.Data).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Resource.DeleteOne(other).Exec(ctx)
	if request("GET", "/sub/"+account.SubscriptionToken+"?target=singbox", nil, "", "").Code != 422 {
		t.Fatal("ambiguous template selection silently accepted")
	}
}
