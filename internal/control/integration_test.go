package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/imzyb/MiGate/internal/subscription"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/imzyb/MiGate/ent"
	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/user"
	"github.com/imzyb/MiGate/packages/contracts"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
)

// Run only against the disposable deploy/test.compose.yml stack.
func TestIntegration(t *testing.T) {
	if os.Getenv("MIGATE_INTEGRATION") != "1" {
		t.Skip("requires isolated local Docker services")
	}
	ctx := context.Background()
	cfg := Config{Listen: "127.0.0.1:19810", EncryptionKey: randomToken(32)}
	path := filepath.Join(t.TempDir(), "config.json")
	token, err := Initialize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, path, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("test")}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := DatabaseConfig{Host: "127.0.0.1", Port: 25432, User: "gate", Password: "local-test-only", Database: "gate"}
	rc := RedisConfig{Address: "127.0.0.1:26379", Username: "migate", Password: "local-test-only"}
	router := s.Router()
	request := func(method, path string, body any, auth, cookie string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	must := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("HTTP %d expected %d: %s", w.Code, code, w.Body.String())
		}
	}
	must(request("POST", "/api/v1/setup/verify", nil, "", ""), 401)
	must(request("POST", "/api/v1/setup/verify", nil, token, ""), 200)
	must(request("POST", "/api/v1/setup/test-db", db, "", ""), 401)
	must(request("POST", "/api/v1/setup/test-db", db, token, ""), 200)
	must(request("POST", "/api/v1/setup/test-redis", rc, token, ""), 200)
	install := map[string]any{"database": db, "redis": rc, "email": "admin@example.test", "password": "Local1-testing-password"}
	installResults := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() { installResults <- request("POST", "/api/v1/setup/install", install, token, "") }()
	}
	aResult, bResult := <-installResults, <-installResults
	if !((aResult.Code == 201 && bResult.Code == 409) || (aResult.Code == 409 && bResult.Code == 201)) {
		t.Fatalf("concurrent install: %d %d", aResult.Code, bResult.Code)
	}
	must(request("POST", "/api/v1/setup/install", install, token, ""), 409)
	must(request("POST", "/api/v1/setup/verify", nil, token, ""), 409)
	// Other applications' Redis keys and administrative commands are denied.
	adminRedis := redis.NewClient(&redis.Options{Addr: rc.Address})
	defer adminRedis.Close()
	if err = adminRedis.Set(ctx, "other-app:sentinel", "preserved", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if s.Redis.Get(ctx, "other-app:sentinel").Err() == nil {
		t.Fatal("ACL exposes another namespace")
	}
	if s.Redis.Do(ctx, "CONFIG", "GET", "port").Err() == nil {
		t.Fatal("ACL allows administration")
	}
	must(request("GET", "/api/v1/admin/users", nil, "", ""), 401)
	login := func(email string) string {
		w := request("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "Local1-testing-password"}, "", "")
		must(w, 200)
		cookies := w.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("no session cookie")
		}
		return cookies[0].Name + "=" + cookies[0].Value
	}
	admin := login("admin@example.test")
	// A VPS identity is independent from web MFA. Pair, approve, sign, reject replay.
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	wPair := request("POST", "/api/v1/devices/pair", map[string]any{"name": "local-device", "public_key": base64.StdEncoding.EncodeToString(pub)}, "", "")
	must(wPair, 201)
	var pair map[string]any
	json.Unmarshal(wPair.Body.Bytes(), &pair)
	id := int(pair["device_id"].(float64))
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/approve", id), map[string]any{"code": pair["pair_code"], "fingerprint": pair["fingerprint"]}, "", admin), 200)
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/approve", id), map[string]any{"code": pair["pair_code"], "fingerprint": pair["fingerprint"]}, "", admin), 400)
	wChallenge := request("POST", "/api/v1/devices/challenge", map[string]any{"device_id": id}, "", "")
	must(wChallenge, 200)
	var challenge map[string]string
	json.Unmarshal(wChallenge.Body.Bytes(), &challenge)
	authBody := map[string]any{"device_id": id, "challenge": challenge["challenge"], "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(challenge["challenge"])))}
	wAuth := request("POST", "/api/v1/devices/auth", authBody, "", "")
	must(wAuth, 200)
	var deviceToken map[string]any
	json.Unmarshal(wAuth.Body.Bytes(), &deviceToken)
	must(request("POST", "/api/v1/devices/auth", authBody, "", ""), 401)
	hostFixture := map[string]any{"source": "linux-procfs-statfs", "observed_at": time.Now().UTC().Format(time.RFC3339), "validity": "available", "logical_cpus": 2, "network_rx": "9007199254740993", "network_tx": "0", "raw_config": "private"}
	must(request("POST", "/api/v1/agent/heartbeat", map[string]any{"host_status": hostFixture}, deviceToken["token"].(string), ""), 200)
	observedDevice, hostErr := s.Store.Device.Get(ctx, id)
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	hostRaw, _ := json.Marshal(observedDevice.Metadata["host_status"])
	if bytes.Contains(hostRaw, []byte("private")) || !bytes.Contains(hostRaw, []byte("9007199254740993")) || !bytes.Contains(hostRaw, []byte("partial")) {
		t.Fatal("host sample projection or exact precision failed")
	}
	must(request("POST", "/api/v1/agent/heartbeat", map[string]any{}, deviceToken["token"].(string), ""), 200)
	observedDevice, _ = s.Store.Device.Get(ctx, id)
	hostRaw, _ = json.Marshal(observedDevice.Metadata["host_status"])
	if !bytes.Contains(hostRaw, []byte("unknown")) || bytes.Contains(hostRaw, []byte("9007199254740993")) {
		t.Fatal("old Agent retained stale telemetry")
	}

	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/revoke", id), map[string]any{}, "", admin), 200)
	must(request("POST", "/api/v1/agent/heartbeat", map[string]any{}, deviceToken["token"].(string), ""), 403)
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/tasks", id), map[string]any{"operation": "core.restart", "confirm": true}, "", admin), 404)
	// Restoring a retained device requires proof of its key and administrator
	// reauthentication. Previous tokens must stay invalid after reapproval.
	recoveryChallenge := request("POST", "/api/v1/devices/recovery/challenge", map[string]any{"device_id": id}, "", "")
	must(recoveryChallenge, 200)
	json.Unmarshal(recoveryChallenge.Body.Bytes(), &challenge)
	proof := map[string]any{"device_id": id, "challenge": challenge["challenge"], "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(challenge["challenge"])))}
	must(request("POST", "/api/v1/devices/recovery", proof, "", ""), 401)
	proof["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(fmt.Sprintf("migate-device-recovery:v1:%d:%s", id, challenge["challenge"]))))
	wRecover := request("POST", "/api/v1/devices/recovery", proof, "", "")
	must(wRecover, 200)
	must(request("POST", "/api/v1/devices/recovery", proof, "", ""), 401)
	var recovered map[string]any
	json.Unmarshal(wRecover.Body.Bytes(), &recovered)
	approval := map[string]any{"code": recovered["pair_code"], "fingerprint": recovered["fingerprint"]}
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/approve", id), approval, "", admin), 401)
	approval["password"] = "Local1-testing-password"
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/approve", id), approval, "", admin), 200)
	must(request("POST", "/api/v1/agent/heartbeat", map[string]any{}, deviceToken["token"].(string), ""), 401)
	must(request("POST", "/api/v1/devices/auth", authBody, "", ""), 401)
	must(request("POST", fmt.Sprintf("/api/v1/admin/devices/%d/revoke", id), map[string]any{}, "", admin), 200)
	must(request("POST", "/api/v1/admin/users", map[string]any{"email": "alice@example.test", "password": "Local1-testing-password", "quota": 2 * contracts.BudgetBlock, "expires": 0}, "", admin), 201)
	must(request("POST", "/api/v1/admin/users", map[string]any{"email": "bob@example.test", "password": "Local1-testing-password", "quota": 2 * contracts.BudgetBlock, "expires": 0}, "", admin), 201)
	alice := login("alice@example.test")
	testUserDirectoryIntegration(t, request, admin, alice)
	testClientCatalogIntegration(t, s, request, admin, alice)
	testAnnouncementIntegration(t, s, request, admin, alice)
	for _, path := range []string{"/api/v1/admin/users", "/api/v1/admin/devices", "/api/v1/admin/grants", "/api/v1/admin/audit", "/api/v1/admin/resources/sources", "/api/v1/admin/settings"} {
		must(request("GET", path, nil, "", alice), 403)
	}
	must(request("POST", "/api/v1/admin/grants", map[string]int{"user_id": 3, "device_id": 1, "inbound_id": 1}, "", alice), 403)
	w := request("GET", "/api/v1/subscription", nil, "", alice)
	must(w, 200)
	if bytes.Contains(w.Body.Bytes(), []byte("bob@example")) {
		t.Fatal("cross-user leak")
	}
	// Observation remains available without any remote model configuration.
	node, err := s.Store.Resource.Create().SetKind("nodes").SetKey("profile-node-fixture").SetData(map[string]any{"url": "ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := s.Store.Resource.Create().SetKind("operators").SetKey("profile-chain-fixture").SetData(map[string]any{"operators": []any{map[string]any{"type": "rename", "params": map[string]any{"template": map[string]any{"enabled": true, "template": "{region:zh}-{index:02}"}}}}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.Store.Resource.Create().SetKind("profiles").SetKey("profile-fixture").SetData(map[string]any{"node_ids": []any{node.ID}, "operator_ids": []any{chain.ID}}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	preview := request("POST", "/api/v1/admin/profiles/preview", map[string]any{"id": profile.ID, "target": "clash"}, "", admin)
	must(preview, 200)
	if !bytes.Contains(preview.Body.Bytes(), []byte("香港-01")) {
		t.Fatal("profile referenced operator not applied")
	}
	must(request("POST", "/api/v1/admin/profiles/preview", map[string]any{"id": profile.ID}, "", alice), 403)
	for _, data := range []map[string]any{
		{"name": "bad list", "node_ids": "1,2"},
		{"name": "wrong resource kind", "source_ids": []any{node.ID}},
		{"name": "duplicate node", "node_ids": []any{node.ID, node.ID}},
		{"name": "fractional ID", "node_ids": []any{1.5}},
	} {
		must(request("POST", "/api/v1/admin/resources/profiles", map[string]any{"data": data}, "", admin), 400)
	}
	must(request("POST", "/api/v1/admin/resources/profiles", map[string]any{"data": map[string]any{"name": "valid references", "node_ids": []any{node.ID}, "operator_ids": []any{chain.ID}}}, "", admin), 200)
	// Unassigned external inventory cannot enter a user's Gate subscription.
	testOutputTemplateIntegration(t, s, request, admin, alice)
	testInventoryIntegration(t, s, request, admin, alice)
	testProvenanceIntegration(t, s, request, admin)
	testSourceOptionsIntegration(t, s, request, admin)
	testResourceDirectoryIntegration(t, s, request, admin, alice)
	testTaskDirectoryIntegration(t, s, request, admin, alice)
	testDeviceFormIntegration(t, s, request, admin)
	testOutboundTransportIntegration(t, s, request, admin, alice)
	testOutboundPools(t, s, request, admin, alice)
	testOutboundPoolSync(t, s, request, admin, alice)
	testPublicPageIntegration(t, request, admin, alice)
	testRoutingOrderIntegration(t, s, request, admin, alice)
	testTrafficHistoryIntegration(t, s, request, admin, alice)
	testOperatorContractIntegration(t, s, request, admin, alice)
	testCertificateHistoryIntegration(t, s, request, admin, alice)
	testCertificateBinding(t, s, request, admin, alice)
	sub := request("GET", "/api/v1/subscription", nil, "", alice)
	must(sub, 200)
	if !bytes.Contains(sub.Body.Bytes(), []byte(`"node_count":0`)) {
		t.Fatal("external node granted without authorization")
	}
	observed, err := s.Store.Device.Create().SetName("offline-observation").SetPublicKey("observation-fixture").SetPairExpires(time.Now()).SetStatus("approved").SetLastSeen(time.Now().Add(-2 * time.Minute)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.observeSecurity(ctx, time.Now())
	event, err := s.Store.Resource.Query().Where(resource.KindEQ("security-event"), resource.KeyEQ(fmt.Sprintf("device:%d", observed.ID))).Only(ctx)
	if err != nil || event.Data["status"] != "unknown" {
		t.Fatal("offline observation missing", err)
	}
	events := request("GET", "/api/v1/admin/events", nil, "", admin)
	must(events, 200)
	if !bytes.Contains(events.Body.Bytes(), []byte("heartbeat_missing_over_60_seconds")) {
		t.Fatal("observation omitted from API")
	}
	s.Store.Device.UpdateOne(observed).SetLastSeen(time.Now()).SetMetadata(map[string]any{"traffic_status": "fresh"}).Save(ctx)
	s.observeSecurity(ctx, time.Now())
	event, err = s.Store.Resource.Get(ctx, event.ID)
	if err != nil || event.Data["status"] != "resolved" {
		t.Fatal("contact restoration not reflected", err)
	}
	// Budget grants for the same user must serialize even across devices.
	u, err := s.Store.User.Query().Where().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var account *ent.User
	for _, v := range u {
		if v.Email == "alice@example.test" {
			account = v
		}
	}
	g1, err := s.Store.Grant.Create().SetUserID(account.ID).SetDeviceID(1001).SetInboundID(1).SetCredential(map[string]any{"uuid": "a"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := s.Store.Grant.Create().SetUserID(account.ID).SetDeviceID(1002).SetInboundID(1).SetCredential(map[string]any{"uuid": "b"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, id := range []int{g1.ID, g2.ID} {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if _, e := s.allocate(ctx, id, 0, 0, ""); e != nil {
				t.Error(e)
			}
		}(id)
	}
	wg.Wait()
	first, err := s.Store.Grant.Get(ctx, g1.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.Store.Grant.Get(ctx, g2.ID)
	if first.Allocated+second.Allocated != account.Quota {
		t.Fatalf("allocation %d + %d != %d", first.Allocated, second.Allocated, account.Quota)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.allocate(ctx, g1.ID, 1024, 1, "epoch-one"); err != nil {
			t.Fatal(err)
		}
	}
	account, _ = s.Store.User.Get(ctx, account.ID)
	if account.Used != 1024 {
		t.Fatalf("duplicate settlement %d", account.Used)
	}
	if _, err = s.allocate(ctx, g1.ID, 1024, 2, "changed-epoch"); err == nil {
		t.Fatal("epoch replay accepted")
	}
	if _, err = s.allocate(ctx, g1.ID, first.Allocated+1, 3, "epoch-one"); err != nil {
		t.Fatal("sampling overshoot must settle actual usage", err)
	}
	account, _ = s.Store.User.Get(ctx, account.ID)
	if account.Used != first.Allocated+1 {
		t.Fatal("overshoot not charged")
	}
	// Source refresh keeps the previous inventory on failure, reports stale state,
	// and never accepts partially recognized subscription contents as successful.
	source, err := s.Store.Resource.Create().SetKind("sources").SetKey("cache-fixture").SetData(map[string]any{"url": "https://example.com/sub", "enabled": true}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	validBody := []byte("ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK")
	cached, err := s.refreshSourceCache(ctx, source.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: validBody}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if links, stale, err := sourceLinks(cached.Data, time.Now()); err != nil || stale || len(links) != 1 {
		t.Fatal("source not fresh", err)
	}
	cached, err = s.refreshSourceCache(ctx, source.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{}, errors.New("test transport failure")
	})
	if err == nil {
		t.Fatal("failed source refresh accepted")
	}
	if links, _, err := sourceLinks(cached.Data, time.Now()); err == nil || len(links) != 0 {
		t.Fatal("stale source served without consent")
	}
	cached.Data["allow_stale"] = true
	if links, stale, err := sourceLinks(cached.Data, time.Now()); err != nil || !stale || len(links) != 1 {
		t.Fatal("previous source inventory lost", err)
	}
	_, err = s.refreshSourceCache(ctx, source.ID, func(context.Context, string, string) (subscription.FetchedInventory, error) {
		return subscription.FetchedInventory{Body: append(validBody, []byte("\nunknown://fixture")...)}, nil
	})
	if err == nil {
		t.Fatal("partially unsupported source marked successful")
	}
	// Completed historical mail must not starve later pending deliveries.
	for i := 0; i < 205; i++ {
		_, err := s.Store.Resource.Create().SetKind("mail").SetKey(fmt.Sprintf("history-%d", i)).SetData(map[string]any{"status": "sent"}).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.Store.Resource.Create().SetKind("mail").SetKey("pending-after-history").SetData(map[string]any{"status": "pending"}).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := s.pendingMail(ctx)
	if err != nil || len(mail) != 1 || mail[0].ID != pending.ID {
		t.Fatalf("pending mail starved by completed history: count=%d err=%v", len(mail), err)
	}
	// Administrator policy permits enrollment but blocks management until MFA.
	testSettingsTransactions(t, s, request, admin, alice)
	testMailWorkerLockRecovery(t, s)
	testDNS01Contracts(t, s, request, admin, alice)
	testDurableDNSCleanup(t, s, request, admin, alice)
	testDNS01Renewal(t, s, request, admin, alice)
	testNodeOrganization(t, s, request, admin, alice)
	testNodeGroups(t, s, request, admin, alice)
	testInventoryDedup(t, s, request, admin, alice)
	testReceiptTransaction(t, s)
	testAuditDirectoryIntegration(t, s, request, admin, alice)
	testEventDirectoryIntegration(t, s, request, admin, alice)
	testProfileOrder(t, s, request, admin, alice)
	testProfilePublication(t, s, request, admin, alice)
	testPublicSubscriptionDirectory(t, s, request, admin, alice)
	testSubscriptionDefaults(t, s, request, admin, alice)
	testSubscriptionNaming(t, s, request, admin, alice)
	testGuestbook(t, s, request, admin, alice)
	testHostHistory(t, s, request, admin, alice)
	testSessionDirectory(t, s, request, admin, alice)
	testAdminSessionsIntegration(t, request, admin, alice)
	must(request("PUT", "/api/v1/admin/settings", map[string]any{"password": "Local1-testing-password", "security": SecuritySettings{TOTP: true, RequireAdminMFA: true}}, "", admin), 200)
	admin = login("admin@example.test")
	must(request("GET", "/api/v1/admin/users", nil, "", admin), 403)
	wSetup := request("POST", "/api/v1/security/totp/setup", map[string]any{"password": "Local1-testing-password"}, "", admin)
	must(wSetup, 200)
	var enrollment map[string]any
	json.Unmarshal(wSetup.Body.Bytes(), &enrollment)
	secret := enrollment["secret"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	confirmResults := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			confirmResults <- request("POST", "/api/v1/security/totp/confirm", map[string]any{"code": code}, "", admin)
		}()
	}
	wConfirm, otherConfirm := <-confirmResults, <-confirmResults
	if wConfirm.Code != 200 {
		wConfirm, otherConfirm = otherConfirm, wConfirm
	}
	must(wConfirm, 200)
	if otherConfirm.Code != 400 && otherConfirm.Code != 401 && otherConfirm.Code != 409 {
		t.Fatalf("concurrent TOTP confirmation must fail, got %d", otherConfirm.Code)
	}
	adminAccount, err := s.Store.User.Query().Where(user.EmailEQ("admin@example.test")).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.Store.Resource.Query().Where(resource.KindEQ("recovery"), resource.OwnerIDEQ(adminAccount.ID)).Count(ctx)
	if err != nil || count != 8 {
		t.Fatalf("concurrent enrollment recovery count=%d err=%v", count, err)
	}
	var enrolled map[string]any
	json.Unmarshal(wConfirm.Body.Bytes(), &enrolled)
	recovery := enrolled["recovery_codes"].([]any)[0].(string)
	must(request("GET", "/api/v1/me", nil, "", admin), 401)
	wLogin := request("POST", "/api/v1/auth/login", map[string]any{"email": "admin@example.test", "password": "Local1-testing-password"}, "", "")
	must(wLogin, 200)
	var factor map[string]any
	json.Unmarshal(wLogin.Body.Bytes(), &factor)
	must(request("POST", "/api/v1/auth/mfa", map[string]any{"challenge": factor["challenge"], "code": code}, "", ""), 401)
	wMFA := request("POST", "/api/v1/auth/mfa", map[string]any{"challenge": factor["challenge"], "code": recovery}, "", "")
	must(wMFA, 200)
	must(request("POST", "/api/v1/auth/mfa", map[string]any{"challenge": factor["challenge"], "code": recovery}, "", ""), 401)
	admin = wMFA.Result().Cookies()[0].Name + "=" + wMFA.Result().Cookies()[0].Value
	must(request("GET", "/api/v1/admin/users", nil, "", admin), 200)
	must(request("POST", "/api/v1/auth/logout", nil, "", admin), 200)
	must(request("GET", "/api/v1/admin/users", nil, "", admin), 401)
	// Deleting only the local completion marker must not permit reinstallation.
	stale := s.Config
	stale.Installed = false
	stale.SetupHash = digest(token)
	stale.SetupExpires = time.Now().Add(time.Minute)
	s.Config = stale
	must(request("POST", "/api/v1/setup/install", install, token, ""), 409)
	// Keep fixtures inside the disposable container, never delete shared schemas here.
}
