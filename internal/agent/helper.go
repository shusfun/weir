package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	runtimecmd "github.com/imzyb/MiGate/internal/runtime/command"
	"github.com/imzyb/MiGate/internal/singbox"
	"github.com/imzyb/MiGate/internal/xray"
)

func (a *Agent) apply(ctx context.Context) error {
	a.State.ApplyPending = true
	b, err := json.Marshal(a.State)
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 75 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/apply", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("local_helper_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("core_validation_or_apply_failed")
	}
	for i := range a.State.Grants {
		a.State.Grants[i].LastRaw = 0
	}
	a.State.ApplyPending = false
	return nil
}
func HelperHandler() http.Handler {
	mux := http.NewServeMux()
	manager := newCoreVersionManager()
	certificates := newCertificateHistoryManager()
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 40*time.Second)
	startupErr := manager.recover(recoveryCtx)
	recoveryCancel()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method", 405)
			return
		}
		if startupErr != nil {
			http.Error(w, "core recovery required", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":true}`))
	})
	var mu sync.Mutex
	mux.HandleFunc("/core-diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var req struct {
			Core string `json:"core"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF || req.Core != "xray" && req.Core != "sing-box" {
			http.Error(w, "invalid core", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		runner := runtimecmd.NewRealCommandRunner(5 * time.Second)
		// journalctl JSON carries metadata alongside up to 200 records. Keep
		// a dedicated bound here; other helper commands retain their 64 KiB cap.
		runner.OutputLimit = 1 << 20
		out := coreDiagnostics(ctx, req.Core, "/etc/migate-gate/cores", "/opt/migate-gate/cores", runner)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/cores", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "GET" {
			http.Error(w, "method", 405)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		out := coreObservation(ctx)
		out["version_management"] = manager.inspect()
		if err := certificates.recover(); err != nil || startupErr != nil {
			out["certificate_history"] = map[string]any{"validity": "unknown", "reason": "certificate_history_recovery_required"}
		} else if history, err := certificates.inspect(); err != nil {
			out["certificate_history"] = map[string]any{"validity": "unknown", "reason": "certificate_history_unavailable"}
		} else {
			out["certificate_history"] = history
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/certificate-clean", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if startupErr != nil {
			http.Error(w, "core recovery required", 503)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var req struct {
			TaskID int    `json:"task_id"`
			Digest string `json:"preview_digest"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := certificates.recover(); err != nil {
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{"message": "certificate_history_recovery_required"})
			return
		}
		result, err := certificates.clean(req.TaskID, req.Digest)
		if err != nil {
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{"message": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("/core-versions", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if startupErr != nil {
			http.Error(w, "core recovery required", 503)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var req coreVersionRequest
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		result, err := manager.execute(ctx, req)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{"message": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("/apply", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if startupErr != nil {
			http.Error(w, "core recovery required", 503)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var state State
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		if json.NewDecoder(r.Body).Decode(&state) != nil {
			http.Error(w, "invalid state", 400)
			return
		}
		applyCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := manager.recover(applyCtx); err != nil {
			http.Error(w, "core recovery required", 503)
			return
		}
		if err := certificates.recover(); err != nil {
			http.Error(w, "certificate recovery required", 503)
			return
		}
		if err := applyState(applyCtx, state); err != nil {
			http.Error(w, "apply failed", 422)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"applied":true}`))
	})
	return mux
}

func HelperHealth(ctx context.Context) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/run/migate/helper.sock")
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://helper/health", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("helper unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("helper not ready")
	}
	return nil
}
func applyState(ctx context.Context, state State) (resultErr error) {
	cleanup, certErr := materializeCertificates(&state)
	if cleanup != nil {
		defer func() {
			if resultErr != nil {
				cleanup()
			}
		}()
	}
	if certErr != nil {
		return certErr
	}
	if len(state.Inbounds) > 100 || len(state.Grants) > 10000 || len(state.Outbounds) > 1000 || len(state.Routing) > 1000 {
		return errors.New("resource limit")
	}
	inbounds := []db.Inbound{}
	ports := map[int]bool{}
	for _, in := range state.Inbounds {
		core := db.InboundCore(in)
		if core != db.CoreXray && core != db.CoreSingbox {
			return errors.New("unsupported inbound core")
		}
		if in.Port < 1024 || in.Port > 65535 || in.Port == 8443 || ports[in.Port] {
			return errors.New("invalid port")
		}
		ports[in.Port] = true
		if !managedProtocol(in.Protocol, core) {
			return errors.New("unsupported protocol")
		}
		if core == db.CoreSingbox && (in.TLSCertFile == "" || in.TLSKeyFile == "") {
			return errors.New("managed TLS certificate required")
		}
		if core == db.CoreSingbox {
			in.Security = "tls"
		}
		if in.TLSCertFile != "" || in.TLSKeyFile != "" {
			base := "/etc/migate-gate/certs/"
			if !filepath.IsAbs(in.TLSCertFile) || !filepath.IsAbs(in.TLSKeyFile) || !within(base, in.TLSCertFile) || !within(base, in.TLSKeyFile) {
				return errors.New("certificate path outside managed directory")
			}
		}
		in.Clients = nil
		for _, g := range state.Grants {
			if int64(g.InboundID) != in.ID || !g.Enabled || !g.DesiredEnabled || g.Used >= g.Allocated || g.Expires > 0 && time.Now().Unix() >= g.Expires {
				continue
			}
			uuid, _ := g.Credential["uuid"].(string)
			password, _ := g.Credential["password"].(string)
			in.Clients = append(in.Clients, db.Client{ID: int64(g.GrantID), UUID: uuid, CredentialID: uuid, Password: password, Email: grantName(g.GrantID), StatsKey: grantName(g.GrantID), Enabled: true})
		}
		// TUIC requires at least one user. Removing the last authorized user
		// must close the listener rather than fail validation and leave access.
		if core == db.CoreSingbox && len(in.Clients) == 0 {
			continue
		}
		inbounds = append(inbounds, in)
	}
	for _, out := range state.Outbounds {
		if out.ID <= 0 || (!db.OutboundSupportsCore(out, db.CoreXray) && !db.OutboundSupportsCore(out, db.CoreSingbox)) {
			return errors.New("unsupported outbound core")
		}
	}
	for i := range state.Routing {
		if state.Routing[i].ClientID > 0 && state.Routing[i].InboundID == 0 {
			for _, g := range state.Grants {
				if int64(g.GrantID) == state.Routing[i].ClientID {
					state.Routing[i].InboundID = int64(g.InboundID)
					break
				}
			}
		}
	}
	for _, r := range state.Routing {
		if r.ID <= 0 || r.RuleSet != "" {
			return errors.New("invalid route")
		}
		if r.InboundID > 0 {
			found := false
			for _, in := range state.Inbounds {
				found = found || in.ID == r.InboundID
			}
			if !found {
				return errors.New("route inbound missing")
			}
		}
		if r.ClientID > 0 {
			found := false
			for _, g := range state.Grants {
				found = found || int64(g.GrantID) == r.ClientID
			}
			if !found {
				return errors.New("route client missing")
			}
		}
	}
	sort.SliceStable(state.Routing, func(i, j int) bool {
		if state.Routing[i].Sort == state.Routing[j].Sort {
			return state.Routing[i].ID < state.Routing[j].ID
		}
		return state.Routing[i].Sort < state.Routing[j].Sort
	})
	// An added outbound must not silently replace the default direct route.
	// Only explicit routing rules select administrator-created proxy outbounds.
	outbounds := append([]db.Outbound{{ID: 0, Tag: "direct", Protocol: "freedom", Enabled: true}}, state.Outbounds...)
	config, err := xray.BuildConfigWithOutbounds(inbounds, outbounds, state.Routing)
	if err != nil {
		return err
	}
	for i := range config.Inbounds {
		if config.Inbounds[i].Tag == "api" {
			config.Inbounds[i].Port = 19815
		}
	}
	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	cores := []coreConfig{{name: "xray", body: b, args: []string{"run", "-test", "-config"}}}
	needsSingbox := false
	for _, in := range inbounds {
		needsSingbox = needsSingbox || db.InboundCore(in) == db.CoreSingbox
	}
	if _, e := os.Stat("/etc/migate-gate/cores/sing-box.json"); e == nil {
		needsSingbox = true
	}
	if needsSingbox {
		cfg, e := singbox.BuildConfigWithOutboundsOptions(inbounds, outbounds, state.Routing, singbox.BuildOptions{EnableV2RayAPIStats: true})
		if e != nil {
			return e
		}
		cfg.Experimental.V2RayAPI.Listen = "127.0.0.1:19817"
		for i := range cfg.Inbounds {
			if cfg.Inbounds[i].TLS == nil || cfg.Inbounds[i].TLS.CertificatePath == singbox.CertFile {
				return errors.New("explicit managed certificate required")
			}
		}
		b, e := json.MarshalIndent(cfg, "", "  ")
		if e != nil {
			return e
		}
		cores = append(cores, coreConfig{name: "sing-box", body: b, args: []string{"check", "-c"}})
	}
	for i := range cores {
		cores[i].disabled = state.CoreDisabled[cores[i].name]
	}
	return applyCoreConfigs(ctx, cores)
}

func verifyCoreReady(ctx context.Context) error {
	return verifyStatsReady(ctx, "xray")
}
func verifyStatsReady(ctx context.Context, core string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	address, engine, service := "127.0.0.1:19815", "xray", "xray.app.stats.command.StatsService"
	if core == "sing-box" {
		address, engine, service = "127.0.0.1:19817", "singbox", "v2ray.core.app.stats.command.StatsService"
	}
	client, err := xray.NewGRPCStatsClientWithEngineAndService(readyCtx, address, engine, service)
	if err != nil {
		return err
	}
	defer client.Close()
	for {
		callCtx, done := context.WithTimeout(readyCtx, 500*time.Millisecond)
		_, err = client.QueryAllStats(callCtx)
		done()
		if err == nil {
			return nil
		}
		select {
		case <-readyCtx.Done():
			return errors.New("core readiness failed")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func grantName(id int) string { return "grant-" + itoa(id) }
func itoa(n int) string       { b, _ := json.Marshal(n); return string(b) }
func within(base, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(path))
	return err == nil && rel != ".." && len(rel) > 0 && !(len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator))
}
func RunHelper(ctx context.Context, socket string) error {
	if socket != "/run/migate/helper.sock" {
		return errors.New("fixed socket required")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0750); err != nil {
		return err
	}
	os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socket, 0660); err != nil {
		return err
	}
	server := &http.Server{Handler: HelperHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 80 * time.Second}
	go func() { <-ctx.Done(); server.Close() }()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

var _ = io.EOF
