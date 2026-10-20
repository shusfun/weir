package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	runtimecmd "github.com/imzyb/MiGate/internal/runtime/command"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (a *Agent) applyCoreVersion(ctx context.Context, t contracts.Task) (map[string]any, error) {
	body, _ := json.Marshal(coreVersionRequest{TaskID: t.ID, Revision: t.Revision, Core: fmtText(t.Payload["core"]), Operation: t.Operation, Version: fmtText(t.Payload["version"])})
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 70 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/core-versions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("core_version_helper_unavailable")
	}
	defer resp.Body.Close()
	var result map[string]any
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 65536)).Decode(&result) != nil {
		return nil, errors.New("core_version_invalid_response")
	}
	if resp.StatusCode != 200 {
		message, _ := result["message"].(string)
		if message == "" {
			message = "core_version_operation_failed"
		}
		return nil, errors.New(message)
	}
	return result, nil
}
func fmtText(v any) string { s, _ := v.(string); return s }

func (a *Agent) cleanCertificateHistory(ctx context.Context, t contracts.Task) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"task_id": t.ID, "preview_digest": fmtText(t.Payload["preview_digest"])})
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/certificate-clean", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("certificate_helper_unavailable")
	}
	defer resp.Body.Close()
	var result map[string]any
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 65536)).Decode(&result) != nil {
		return nil, errors.New("certificate_history_invalid_response")
	}
	if resp.StatusCode != 200 {
		message := fmtText(result["message"])
		if message == "" {
			message = "certificate_cleanup_failed"
		}
		return nil, errors.New(message)
	}
	return result, nil
}

func coreObservation(ctx context.Context) map[string]any {
	runner := runtimecmd.NewRealCommandRunner(2 * time.Second)
	items := []map[string]any{}
	for _, core := range []string{"xray", "sing-box"} {
		entry := map[string]any{"core": core, "state": "unknown", "version": "unknown", "installed": false}
		binary := "/opt/migate-gate/cores/" + core
		if info, err := os.Stat(binary); err == nil && info.Mode().IsRegular() {
			entry["installed"] = true
			if out, e := runner.RunOutput(ctx, binary, "version"); e == nil {
				version := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
				if len(version) <= 200 {
					entry["version"] = version
				}
			}
		}
		out, err := runner.RunOutput(ctx, "systemctl", "is-active", "migate-gate-"+core+".service")
		state := strings.TrimSpace(string(out))
		if err == nil && state == "active" {
			entry["state"] = "active"
		} else if state == "inactive" || state == "failed" {
			entry["state"] = state
		}
		items = append(items, entry)
	}
	return map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339), "source": "gate-helper", "items": items}
}

func (a *Agent) readCoreObservation(ctx context.Context) (map[string]any, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://helper/cores", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("core_observation_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("core_observation_unavailable")
	}
	var result map[string]any
	err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 65536)).Decode(&result)
	return result, err
}
