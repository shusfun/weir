package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	runtimecmd "github.com/imzyb/MiGate/internal/runtime/command"
	"github.com/imzyb/MiGate/packages/contracts"
)

const lifecycleUninstallOperation = "lifecycle.uninstall"

func UninstallCommand(mode string) ([]string, error) {
	base := []string{"uninstall", "--manifest", "/etc/migate-gate/install-manifest.json"}
	switch mode {
	case "retain":
		return append(base, "--confirm", "gate:uninstall"), nil
	case "cores":
		return append(base, "--confirm", "gate:uninstall:with-cores", "--with-cores"), nil
	case "purge":
		return append(base, "--confirm", "gate:uninstall:purge", "--purge", "--backup-dir", "/var/backups/migate"), nil
	default:
		return nil, errors.New("uninstall_mode_refused")
	}
}

func decodeUninstall(payload map[string]any) (string, error) {
	for key := range payload {
		if key != "mode" {
			return "", errors.New("uninstall_payload_refused")
		}
	}
	return UninstallMode(fmtText(payload["mode"]))
}

func UninstallMode(mode string) (string, error) {
	if _, err := UninstallCommand(mode); err != nil {
		return "", err
	}
	return mode, nil
}

func (a *Agent) uninstallLocal(ctx context.Context, t contracts.Task) (map[string]any, error) {
	body, err := json.Marshal(t.Payload)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 70 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/uninstall", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("uninstall_helper_unavailable")
	}
	defer resp.Body.Close()
	var result map[string]any
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&result) != nil {
		return nil, errors.New("uninstall_invalid_response")
	}
	if resp.StatusCode != 200 {
		message := fmtText(result["message"])
		if message == "" {
			message = "uninstall_failed"
		}
		return nil, errors.New(message)
	}
	return result, nil
}

func runGateUninstall(args []string) error {
	return runtimecmd.NewRealCommandRunner(60*time.Second).Run(context.Background(), "/opt/migate-gate/migate-manage", args...)
}

func uninstallHandler(run func([]string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var payload map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload) != nil {
			writeReleaseError(w, errors.New("uninstall_payload_refused"))
			return
		}
		mode, err := decodeUninstall(payload)
		if err != nil {
			writeReleaseError(w, err)
			return
		}
		args, err := UninstallCommand(mode)
		if err != nil {
			writeReleaseError(w, err)
			return
		}
		if err = run(args); err != nil {
			writeReleaseError(w, errors.New("uninstall_failed"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"applied":true}`))
	}
}
