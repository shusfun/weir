package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/internal/xray"
	"github.com/imzyb/MiGate/packages/contracts"
	_ "modernc.org/sqlite"
)

type Config struct {
	Center            string `json:"center"`
	DeviceID          int    `json:"device_id"`
	PrivateKey        string `json:"private_key"`
	Name              string `json:"name"`
	Socket            string `json:"socket"`
	DataDir           string `json:"data_dir"`
	TLSServerName     string `json:"tls_server_name,omitempty"`
	CenterDialAddress string `json:"center_dial_address,omitempty"`
}
type LocalGrant struct {
	GrantID        int            `json:"grant_id"`
	InboundID      int            `json:"inbound_id"`
	Credential     map[string]any `json:"credential"`
	Allocated      int64          `json:"allocated"`
	Used           int64          `json:"used"`
	Expires        int64          `json:"expires"`
	Enabled        bool           `json:"enabled"`
	DesiredEnabled bool           `json:"desired_enabled"`
	LastRaw        int64          `json:"last_raw"`
	Sequence       int64          `json:"sequence"`
	Epoch          string         `json:"epoch"`
	Revision       int64          `json:"revision"`
}
type State struct {
	CoreDisabled map[string]bool      `json:"core_disabled,omitempty"`
	ApplyPending bool                 `json:"apply_pending"`
	Versions     map[string]int64     `json:"versions"`
	Inbounds     []db.Inbound         `json:"inbounds"`
	Grants       []LocalGrant         `json:"grants"`
	Outbounds    []db.Outbound        `json:"outbounds"`
	Routing      []db.RoutingRule     `json:"routing"`
	Certificates []ManagedCertificate `json:"certificates,omitempty"`
}
type Agent struct {
	hostCPU       hostCPUSampler
	stats         xray.StatsClient
	singStats     xray.StatsClient
	Config        Config
	DB            *sql.DB
	HTTP          *http.Client
	token         string
	tokenUntil    time.Time
	mu            sync.Mutex
	State         State
	TrafficStatus string
}

func token(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Load(path string) (Config, error) {
	var c Config
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if e != nil {
		return c, e
	}
	u, e := url.Parse(c.Center)
	if e != nil || u.Scheme != "https" {
		return c, errors.New("HTTPS center required")
	}
	return c, nil
}
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(path+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func New(c Config) (*Agent, error) {
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", filepath.Join(c.DataDir, "agent.db"))
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "CREATE TABLE IF NOT EXISTS state(id INTEGER PRIMARY KEY CHECK(id=1), data TEXT NOT NULL)", "CREATE TABLE IF NOT EXISTS receipts(id INTEGER PRIMARY KEY, revision INTEGER NOT NULL, result TEXT NOT NULL)"} {
		if _, err = database.Exec(q); err != nil {
			return nil, err
		}
	}
	client, err := centerClient(c, 30*time.Second)
	if err != nil {
		database.Close()
		return nil, err
	}
	a := &Agent{Config: c, DB: database, TrafficStatus: "unknown", HTTP: client}
	a.stats, err = xray.NewGRPCStatsClient(context.Background(), "127.0.0.1:19815")
	if err != nil {
		database.Close()
		return nil, err
	}
	a.singStats, err = xray.NewGRPCStatsClientWithEngineAndService(context.Background(), "127.0.0.1:19817", "singbox", "v2ray.core.app.stats.command.StatsService")
	if err != nil {
		a.stats.Close()
		database.Close()
		return nil, err
	}
	var raw string
	if database.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw) == nil {
		if err = json.Unmarshal([]byte(raw), &a.State); err != nil {
			return nil, err
		}
	}
	// Reconcile persisted desired state after a restart, including a prior
	// helper failure. A disabled flag alone is not proof of core enforcement.
	a.State.ApplyPending = true
	return a, nil
}
func (a *Agent) persist() error {
	b, err := json.Marshal(a.State)
	if err != nil {
		return err
	}
	_, err = a.DB.Exec("INSERT INTO state(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", b)
	return err
}
func (a *Agent) request(ctx context.Context, method, path string, body, out any, auth bool) error {
	var data io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		data = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.Config.Center+"/api/v1"+path, data)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		a.mu.Lock()
		t := a.token
		a.mu.Unlock()
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 401 {
			a.mu.Lock()
			a.tokenUntil = time.Time{}
			a.mu.Unlock()
		}
		return fmt.Errorf("center HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	}
	return nil
}
func centerClient(c Config, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.CenterDialAddress != "" {
		host, _, err := net.SplitHostPort(c.CenterDialAddress)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return nil, errors.New("center dial address must be an explicit loopback socket")
		}
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, c.CenterDialAddress)
		}
	}
	if c.TLSServerName != "" {
		u, err := url.Parse(c.Center)
		if err != nil || !strings.EqualFold(u.Hostname(), c.TLSServerName) {
			return nil, errors.New("TLS server name must match center hostname, allowing case differences only")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.TLSServerName}
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func Pair(ctx context.Context, center, name, path, tlsName, dialAddress string) (map[string]any, error) {
	u, parseErr := url.Parse(center)
	if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("HTTPS center origin required")
	}
	center = strings.TrimRight(center, "/")
	if _, err := os.Stat(path); err == nil {
		return nil, errors.New("device configuration exists; revoke before pairing again")
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	c := Config{Center: center, Name: name, PrivateKey: base64.StdEncoding.EncodeToString(private), Socket: "/run/migate/helper.sock", DataDir: "/var/lib/migate-gate", TLSServerName: tlsName, CenterDialAddress: dialAddress}
	client, err := centerClient(c, 15*time.Second)
	if err != nil {
		return nil, err
	}
	a := &Agent{Config: c, HTTP: client}
	var result map[string]any
	if err = a.request(ctx, "POST", "/devices/pair", map[string]any{"name": name, "public_key": base64.StdEncoding.EncodeToString(pub)}, &result, false); err != nil {
		return nil, err
	}
	c.DeviceID = int(result["device_id"].(float64))
	return result, Save(path, c)
}
func (a *Agent) authorize(ctx context.Context) error {
	a.mu.Lock()
	fresh := time.Now().Before(a.tokenUntil)
	a.mu.Unlock()
	if fresh {
		return nil
	}
	var challenge struct {
		Challenge string `json:"challenge"`
	}
	if err := a.request(ctx, "POST", "/devices/challenge", map[string]any{"device_id": a.Config.DeviceID}, &challenge, false); err != nil {
		return err
	}
	private, err := base64.StdEncoding.DecodeString(a.Config.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return errors.New("invalid private key")
	}
	sig := ed25519.Sign(private, []byte(challenge.Challenge))
	var auth struct {
		Token string `json:"token"`
	}
	if err = a.request(ctx, "POST", "/devices/auth", map[string]any{"device_id": a.Config.DeviceID, "challenge": challenge.Challenge, "signature": base64.StdEncoding.EncodeToString(sig)}, &auth, false); err != nil {
		return err
	}
	a.mu.Lock()
	a.token = auth.Token
	a.tokenUntil = time.Now().Add(14 * time.Minute)
	a.mu.Unlock()
	return nil
}

// Recover retains the device ID and local metering state. Possession of the
// original private key only requests recovery; an administrator must reapprove.
func Recover(ctx context.Context, path string) (map[string]any, error) {
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	private, err := base64.StdEncoding.DecodeString(c.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key")
	}
	client, err := centerClient(c, 15*time.Second)
	if err != nil {
		return nil, err
	}
	a := &Agent{Config: c, HTTP: client}
	var challenge struct {
		Challenge string `json:"challenge"`
	}
	if err = a.request(ctx, "POST", "/devices/recovery/challenge", map[string]any{"device_id": c.DeviceID}, &challenge, false); err != nil {
		return nil, err
	}
	message := fmt.Sprintf("migate-device-recovery:v1:%d:%s", c.DeviceID, challenge.Challenge)
	signature := ed25519.Sign(ed25519.PrivateKey(private), []byte(message))
	var result map[string]any
	err = a.request(ctx, "POST", "/devices/recovery", map[string]any{"device_id": c.DeviceID, "challenge": challenge.Challenge, "signature": base64.StdEncoding.EncodeToString(signature)}, &result, false)
	return result, err
}
func (a *Agent) Run(ctx context.Context) error {
	done := make(chan struct{})
	go func() { defer close(done); a.background(ctx) }()
	defer func() {
		<-done
		if a.stats != nil {
			a.stats.Close()
		}
		if a.singStats != nil {
			a.singStats.Close()
		}
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := a.authorize(ctx); err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		var response struct {
			Items []contracts.Task `json:"items"`
		}
		if err := a.request(ctx, "GET", "/agent/tasks", nil, &response, true); err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, task := range response.Items {
			result := a.execute(ctx, task)
			if err := a.request(ctx, "POST", "/agent/results", result, nil, true); err != nil {
				continue
			}
		}
	}
}

// Revoke is used during explicit local uninstall; it never stops other devices.
func (a *Agent) Revoke(ctx context.Context) error {
	if err := a.authorize(ctx); err != nil {
		return err
	}
	return a.request(ctx, "POST", "/agent/revoke", map[string]any{}, nil, true)
}
func (a *Agent) execute(ctx context.Context, t contracts.Task) contracts.Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := contracts.Result{TaskID: t.ID, Revision: t.Revision, Status: "failed", Data: map[string]any{"message": "execution_failed"}}
	if t.DeviceID != a.Config.DeviceID {
		result.Data["message"] = "invalid_target"
		return result
	}
	var receipt string
	var revision int64
	if a.DB.QueryRow("SELECT revision,result FROM receipts WHERE id=?", t.ID).Scan(&revision, &receipt) == nil {
		if revision == t.Revision {
			json.Unmarshal([]byte(receipt), &result)
		}
		return result
	}
	if t.DeviceID != a.Config.DeviceID || time.Now().After(t.ExpiresAt) {
		result.Data["message"] = "invalid_target_or_expired"
		return result
	}
	// Settle the current counters before any operation that may restart the core.
	readOnly := t.Operation == "core.status" || t.Operation == "core.inspect" || t.Operation == "outbound.probe" || t.Operation == "outbound.catalog.probe"
	stateOnly := t.Operation == "outbound.reorder"
	if !readOnly && !stateOnly {
		a.collectLocked(ctx)
	}
	old, _ := json.Marshal(a.State)
	var err error
	if t.Operation != "outbound.probe" && t.Operation != "outbound.catalog.probe" {
		err = a.mutate(t)
	}
	var observation map[string]any
	versionOperation := t.Operation == "core.install" || t.Operation == "core.rollback" || t.Operation == "core.clean"
	certificateCleanup := t.Operation == "certificate.clean"
	releaseReplace := t.Operation == releaseReplaceOperation
	uninstalling := t.Operation == lifecycleUninstallOperation
	if err == nil && certificateCleanup {
		observation, err = a.cleanCertificateHistory(ctx, t)
	}
	if err == nil && versionOperation {
		observation, err = a.applyCoreVersion(ctx, t)
		if err == nil && observation["service_restarted"] == true && observation["replayed"] != true {
			for i := range a.State.Grants {
				for _, in := range a.State.Inbounds {
					if int(in.ID) == a.State.Grants[i].InboundID && db.InboundCore(in) == fmtText(t.Payload["core"]) {
						a.State.Grants[i].LastRaw = 0
						break
					}
				}
			}
		}
	}
	if err == nil && t.Operation == "core.status" {
		observation, err = a.readCoreObservation(ctx)
	}
	if err == nil && t.Operation == "core.inspect" {
		observation, err = a.readCoreDiagnostics(ctx, fmtText(t.Payload["core"]))
	}
	if err == nil && t.Operation == "outbound.probe" {
		observation, err = a.probeOutbounds(ctx, t)
	}
	if err == nil && t.Operation == "outbound.catalog.probe" {
		observation, err = a.probeCatalog(ctx, t)
	}
	if err == nil && releaseReplace {
		observation, err = a.replaceRelease(ctx, t)
	}
	if err == nil && uninstalling {
		observation, err = a.uninstallLocal(ctx, t)
	}
	if err == nil && !readOnly && !stateOnly && !versionOperation && !certificateCleanup && !releaseReplace && !uninstalling {
		err = a.apply(ctx)
	}
	if err != nil {
		if !readOnly {
			json.Unmarshal(old, &a.State)
		}
		result.Data["message"] = err.Error()
	} else {
		result.Status = "applied"
		result.Data = map[string]any{"applied": true}
		if observation != nil {
			result.Data = observation
		}
		if !readOnly {
			err = a.persist()
		}
		if err != nil {
			json.Unmarshal(old, &a.State)
			if !certificateCleanup && !readOnly && !stateOnly {
				_ = a.apply(ctx)
			}
			result.Status = "failed"
			result.Data["message"] = "state_persistence_failed"
		}
	}
	b, _ := json.Marshal(result)
	if _, err := a.DB.Exec("INSERT OR REPLACE INTO receipts(id,revision,result) VALUES(?,?,?)", t.ID, t.Revision, string(b)); err != nil {
		result.Status = "failed"
		result.Data = map[string]any{"message": "receipt_persistence_failed"}
	}
	return result
}
func decode(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
func (a *Agent) mutate(t contracts.Task) error {
	resourceKey := ""
	if strings.HasPrefix(t.Operation, "inbound.") || strings.HasPrefix(t.Operation, "outbound.") || strings.HasPrefix(t.Operation, "routing.") || strings.HasPrefix(t.Operation, "certificate.") && t.Operation != "certificate.clean" {
		resourceKey = fmt.Sprintf("%s:%d", strings.SplitN(t.Operation, ".", 2)[0], num(t.Payload["id"]))
		if a.State.Versions[resourceKey] > t.Revision {
			return errors.New("stale_resource_revision")
		}
	}
	switch t.Operation {
	case "outbound.probe":
		return nil
	case "outbound.reorder":
		if err := a.reorderOutbounds(t); err != nil {
			return err
		}
	case "outbound.pool.apply":
		return a.applyOutboundPool(t)
	case "routing.reorder":
		if err := a.reorderRoutes(t); err != nil {
			return err
		}
	case "certificate.bind":
		return a.bindCertificate(t)
	case "certificate.upsert":
		var certificate ManagedCertificate
		if err := decode(t.Payload, &certificate); err != nil {
			return err
		}
		if _, err := certificate.Validate(); err != nil {
			return err
		}
		found := false
		for i, existing := range a.State.Certificates {
			if existing.ID == certificate.ID {
				a.State.Certificates[i] = certificate
				found = true
			}
		}
		if !found {
			if len(a.State.Certificates) >= 16 {
				return errors.New("certificate limit")
			}
			a.State.Certificates = append(a.State.Certificates, certificate)
		}
	case "certificate.delete":
		id := num(t.Payload["id"])
		for _, in := range a.State.Inbounds {
			if in.CertificateID == id {
				return errors.New("certificate_has_inbounds")
			}
		}
		kept := []ManagedCertificate{}
		for _, certificate := range a.State.Certificates {
			if certificate.ID != id {
				kept = append(kept, certificate)
			}
		}
		a.State.Certificates = kept
	case "outbound.upsert":
		var out db.Outbound
		if err := decode(t.Payload, &out); err != nil {
			return err
		}
		if out.ID <= 0 || out.Tag == "" || len(out.Tag) > 100 {
			return errors.New("invalid_outbound")
		}
		if out.Source == db.OutboundSourceSubscription || out.SubscriptionID != 0 || out.SubscriptionIdentity != "" {
			return errors.New("managed_pool_member_use_pool")
		}
		for _, existing := range a.State.Outbounds {
			if existing.ID == out.ID && existing.Source == db.OutboundSourceSubscription {
				return errors.New("managed_pool_member_use_pool")
			}
		}
		if err := db.ValidateOutboundProfile(out); err != nil {
			return errors.New("invalid_outbound_profile")
		}
		settings, err := db.ManagedOutboundSettings(out)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(settings)
		if err != nil {
			return errors.New("invalid_outbound_transport")
		}
		out.SettingsJSON = string(encoded)
		out.Enabled = true
		for _, old := range a.State.Outbounds {
			if old.ID == out.ID {
				out.Enabled = old.Enabled
				out.Sort = old.Sort
			}
		}
		if value, supplied := t.Payload["enabled"]; supplied {
			enabled, ok := value.(bool)
			if !ok {
				return errors.New("invalid_resource_enabled")
			}
			out.Enabled = enabled
		}
		if !out.Enabled {
			for _, rule := range a.State.Routing {
				if rule.Enabled && rule.OutboundID == out.ID {
					return errors.New("outbound_has_enabled_routes")
				}
			}
		}
		found := false
		for i, v := range a.State.Outbounds {
			if v.ID != out.ID && v.Tag == out.Tag {
				return errors.New("duplicate_outbound_tag")
			}
			if v.ID == out.ID {
				a.State.Outbounds[i] = out
				found = true
			}
		}
		if !found {
			a.State.Outbounds = append(a.State.Outbounds, out)
		}
	case "outbound.delete":
		id := num(t.Payload["id"])
		for _, out := range a.State.Outbounds {
			if out.ID == id && out.Source == db.OutboundSourceSubscription {
				return errors.New("managed_pool_member_use_pool")
			}
		}
		for _, r := range a.State.Routing {
			if r.Enabled && r.OutboundID == id {
				return errors.New("outbound_has_routes")
			}
		}
		kept := []db.Outbound{}
		for _, v := range a.State.Outbounds {
			if v.ID != id {
				kept = append(kept, v)
			}
		}
		a.State.Outbounds = kept
	case "routing.upsert":
		var rule db.RoutingRule
		if err := decode(t.Payload, &rule); err != nil {
			return err
		}
		if rule.ID <= 0 || rule.OutboundID <= 0 || rule.RuleSet != "" {
			return errors.New("invalid_route_or_external_ruleset")
		}
		found := false
		rule.Enabled = true
		for _, old := range a.State.Routing {
			if old.ID == rule.ID {
				rule.Enabled = old.Enabled
			}
		}
		if value, supplied := t.Payload["enabled"]; supplied {
			enabled, ok := value.(bool)
			if !ok {
				return errors.New("invalid_resource_enabled")
			}
			rule.Enabled = enabled
		}
		for _, out := range a.State.Outbounds {
			found = found || out.ID == rule.OutboundID && (out.Enabled || !rule.Enabled)
		}
		if !found {
			return errors.New("route_outbound_missing")
		}
		found = false
		for i, v := range a.State.Routing {
			if v.ID == rule.ID {
				a.State.Routing[i] = rule
				found = true
			}
		}
		if !found {
			a.State.Routing = append(a.State.Routing, rule)
		}
	case "routing.delete":
		id := num(t.Payload["id"])
		kept := []db.RoutingRule{}
		for _, v := range a.State.Routing {
			if v.ID != id {
				kept = append(kept, v)
			}
		}
		a.State.Routing = kept
	case "inbound.upsert":
		var inbound db.Inbound
		if err := decode(t.Payload, &inbound); err != nil {
			return err
		}
		if inbound.ID <= 0 || inbound.Port == 80 || inbound.Port == 443 || inbound.Port == 8443 || inbound.Port < 1024 || inbound.Port > 65535 {
			return errors.New("invalid_or_reserved_inbound_port")
		}
		if !managedProtocol(inbound.Protocol, db.InboundCore(inbound)) {
			return errors.New("protocol_capability_not_yet_enabled")
		}
		inbound.Enabled = true
		for _, old := range a.State.Inbounds {
			if old.ID == inbound.ID {
				inbound.Enabled = old.Enabled
			}
		}
		if value, supplied := t.Payload["enabled"]; supplied {
			enabled, ok := value.(bool)
			if !ok {
				return errors.New("invalid_resource_enabled")
			}
			inbound.Enabled = enabled
		}
		found := false
		for i, v := range a.State.Inbounds {
			if v.ID == inbound.ID {
				a.State.Inbounds[i] = inbound
				found = true
			}
			if v.ID != inbound.ID && v.Port == inbound.Port {
				return errors.New("port_already_assigned")
			}
		}
		if !found {
			a.State.Inbounds = append(a.State.Inbounds, inbound)
		}
	case "inbound.delete":
		id := num(t.Payload["id"])
		for _, g := range a.State.Grants {
			if int64(g.InboundID) == id && g.DesiredEnabled {
				return errors.New("inbound_has_active_grants")
			}
		}
		kept := []db.Inbound{}
		for _, in := range a.State.Inbounds {
			if in.ID != id {
				kept = append(kept, in)
			}
		}
		a.State.Inbounds = kept
	case "grant.upsert", "grant.revoke":
		id := int(num(t.Payload["grant_id"]))
		idx := -1
		for i, g := range a.State.Grants {
			if g.GrantID == id {
				idx = i
				if g.Revision > t.Revision {
					return errors.New("stale_revision")
				}
				break
			}
		}
		if idx < 0 {
			if t.Operation == "grant.revoke" {
				return nil
			}
			a.State.Grants = append(a.State.Grants, LocalGrant{GrantID: id, Epoch: token(16)})
			idx = len(a.State.Grants) - 1
		}
		g := &a.State.Grants[idx]
		g.Revision = t.Revision
		if t.Operation == "grant.revoke" {
			g.DesiredEnabled = false
			g.Enabled = false
			break
		}
		g.InboundID = int(num(t.Payload["inbound_id"]))
		var cred map[string]any
		decode(t.Payload["credential"], &cred)
		g.Credential = cred
		g.Allocated = max(g.Allocated, num(t.Payload["allocated"]))
		g.Expires = num(t.Payload["expires"])
		g.DesiredEnabled, _ = t.Payload["enabled"].(bool)
		g.Enabled = g.DesiredEnabled && g.Used < g.Allocated && (g.Expires == 0 || time.Now().Unix() < g.Expires)
	case "core.start", "core.stop":
		core, _ := t.Payload["core"].(string)
		if core != "xray" && core != "sing-box" {
			return errors.New("unknown_managed_core")
		}
		if a.State.CoreDisabled == nil {
			a.State.CoreDisabled = map[string]bool{}
		}
		a.State.CoreDisabled[core] = t.Operation == "core.stop"
		return nil
	case "core.inspect":
		if len(t.Payload) != 1 || fmtText(t.Payload["core"]) != "xray" && fmtText(t.Payload["core"]) != "sing-box" {
			return errors.New("invalid_managed_core")
		}
	case "core.restart", "core.status", "core.install", "core.rollback", "core.clean", "certificate.clean", releaseReplaceOperation, lifecycleUninstallOperation:
		return nil
	default:
		return errors.New("operation_capability_not_yet_enabled")
	}
	if resourceKey != "" {
		if a.State.Versions == nil {
			a.State.Versions = map[string]int64{}
		}
		a.State.Versions[resourceKey] = t.Revision
	}
	return nil
}
func (a *Agent) background(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); periodic(ctx, 15*time.Second, a.sendHeartbeat) }()
	go func() { defer wg.Done(); periodic(ctx, 30*time.Second, a.report) }()
	periodic(ctx, time.Second, a.sample)
	wg.Wait()
}
func periodic(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}
func (a *Agent) sample(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := a.collectLocked(ctx)
	if changed {
		a.State.ApplyPending = true
	}
	if err := a.persist(); err != nil {
		a.TrafficStatus = "persistence_error"
		return
	}
	if a.State.ApplyPending {
		if err := a.apply(ctx); err != nil {
			a.TrafficStatus = "enforcement_error"
		} else {
			_ = a.persist()
		}
	}
}

func (a *Agent) collectLocked(ctx context.Context) bool {
	queryCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	var stats map[string]*xray.ClientStats
	var err error
	if a.stats == nil {
		err = errors.New("stats unavailable")
	} else {
		stats, err = a.stats.QueryAllStats(queryCtx)
	}
	if err != nil {
		a.TrafficStatus = "unknown"
	} else {
		a.TrafficStatus = "fresh"
	}
	var singStats map[string]*xray.ClientStats
	var singErr error
	needsSingbox := false
	inboundCore := map[int]string{}
	certificateValid := map[int64]bool{}
	for _, cert := range a.State.Certificates {
		_, e := cert.Validate()
		certificateValid[cert.ID] = e == nil
	}
	inboundCertificateValid := map[int]bool{}
	for _, in := range a.State.Inbounds {
		inboundCore[int(in.ID)] = db.InboundCore(in)
		inboundCertificateValid[int(in.ID)] = in.Enabled && (in.CertificateID == 0 || certificateValid[in.CertificateID])
		needsSingbox = needsSingbox || db.InboundCore(in) == db.CoreSingbox
	}
	if needsSingbox {
		if a.singStats == nil {
			singErr = errors.New("singbox stats unavailable")
		} else {
			singCtx, done := context.WithTimeout(ctx, 800*time.Millisecond)
			singStats, singErr = a.singStats.QueryAllStats(singCtx)
			done()
		}
		if singErr != nil {
			a.TrafficStatus = "unknown"
		}
	}
	changed := false
	for i := range a.State.Grants {
		g := &a.State.Grants[i]
		counters, statsErr := stats, err
		if inboundCore[g.InboundID] == db.CoreSingbox {
			counters, statsErr = singStats, singErr
		}
		stat, ok := counters[fmt.Sprintf("grant-%d", g.GrantID)]
		if ok {
			raw := stat.Uplink + stat.Downlink
			if raw >= g.LastRaw {
				g.Used += raw - g.LastRaw
			} else {
				g.Used += raw
			}
			g.LastRaw = raw
		}
		// Missing counters cannot silently bypass quota/expiry enforcement.
		enabled := !a.State.CoreDisabled[inboundCore[g.InboundID]] && statsErr == nil && inboundCertificateValid[g.InboundID] && g.DesiredEnabled && g.Used < g.Allocated && (g.Expires == 0 || time.Now().Unix() < g.Expires)
		if enabled != g.Enabled {
			g.Enabled = enabled
			changed = true
		}
	}
	return changed
}
func (a *Agent) sendHeartbeat(ctx context.Context) {
	a.mu.Lock()
	inbounds := []map[string]any{}
	for _, v := range a.State.Inbounds {
		inbounds = append(inbounds, map[string]any{"id": v.ID, "remark": v.Remark, "protocol": v.Protocol, "port": v.Port, "enabled": v.Enabled})
	}
	status := a.TrafficStatus
	a.mu.Unlock()
	observation, err := a.readCoreObservation(ctx)
	if err != nil {
		observation = map[string]any{"source": "gate-helper", "observed_at": time.Now().UTC().Format(time.RFC3339), "validity": "unknown"}
	}
	a.request(ctx, "POST", "/agent/heartbeat", map[string]any{"version": buildinfo.Version, "hostname": a.Config.Name, "traffic_status": status, "inbounds": inbounds, "core_status": observation, "host_status": a.observeHost(), "capabilities": []string{"core.control.v1", "core.inspect.v1", "core.diagnostics.v1", "core.versions.v1", "certificate.delivery.v1", "certificate.bind.v1", "certificate.history.v1", "routing.order.v1", "outbound.transport.v1", "outbound.trust.v1", "outbound.pool.v1", "outbound.tools.v1", "resource.enabled.v1", "inbound.enabled.v1", "host.observation.v1", "host.cpu.v1", "release.replace.v1", "lifecycle.uninstall.v1"}}, nil, true)
}
func (a *Agent) report(ctx context.Context) {
	a.mu.Lock()
	items := []contracts.Meter{}
	for i := range a.State.Grants {
		g := &a.State.Grants[i]
		g.Sequence++
		items = append(items, contracts.Meter{GrantID: g.GrantID, Sequence: g.Sequence, Epoch: g.Epoch, Used: g.Used})
	}
	if err := a.persist(); err != nil {
		a.TrafficStatus = "persistence_error"
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	var response struct {
		Items []contracts.Budget `json:"items"`
	}
	if err := a.request(ctx, "POST", "/agent/meter", map[string]any{"items": items}, &response, true); err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := false
	for _, b := range response.Items {
		for i := range a.State.Grants {
			g := &a.State.Grants[i]
			if g.GrantID != b.GrantID {
				continue
			}
			g.Allocated = max(g.Allocated, b.Allocated)
			g.Expires = b.Expires
			g.DesiredEnabled = b.Enabled
			enabled := g.DesiredEnabled && g.Used < g.Allocated && (g.Expires == 0 || time.Now().Unix() < g.Expires)
			if enabled != g.Enabled {
				g.Enabled = enabled
				changed = true
			}
		}
	}
	a.persist()
	if changed {
		a.collectLocked(ctx)
		if err := a.persist(); err != nil {
			a.TrafficStatus = "persistence_error"
			return
		}
		if err := a.apply(ctx); err != nil {
			a.TrafficStatus = "enforcement_error"
		} else {
			a.persist()
		}
	}
}
