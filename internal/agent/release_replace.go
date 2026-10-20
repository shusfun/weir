package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/packages/contracts"
)

const releaseReplaceOperation = "release.replace"

func (a *Agent) replaceRelease(ctx context.Context, t contracts.Task) (map[string]any, error) {
	body, err := json.Marshal(t.Payload)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 70 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/release-replace", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("release_helper_unavailable")
	}
	defer resp.Body.Close()
	var result map[string]any
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&result) != nil {
		return nil, errors.New("release_replace_invalid_response")
	}
	if resp.StatusCode != 200 {
		message := fmtText(result["message"])
		if message == "" {
			message = "release_replace_failed"
		}
		return nil, errors.New(message)
	}
	return result, nil
}

func InstallRelease(root, version string, gate, manage []byte, gateSum, manageSum string, restart func() error) error {
	parsed, ok := buildinfo.ParseScheme(version)
	if !ok || parsed.String() != version {
		return errors.New("invalid_release_version")
	}
	if err := matchSum(gate, gateSum); err != nil {
		return err
	}
	if err := matchSum(manage, manageSum); err != nil {
		return err
	}
	releaseDir := filepath.Join(root, "releases", version)
	if !within(root, releaseDir) || filepath.Base(releaseDir) == "cores" {
		return errors.New("release_path_refused")
	}
	current := filepath.Join(root, "current")
	if sameRelease(current, releaseDir, filepath.Join(releaseDir, "migate-gate"), gate) {
		return nil
	}
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		return err
	}
	if err := writeExecutable(filepath.Join(releaseDir, "migate-gate"), gate); err != nil {
		return err
	}
	if err := writeExecutable(filepath.Join(releaseDir, "migate-manage"), manage); err != nil {
		return err
	}
	previous := filepath.Join(root, "previous")
	if link, err := os.Readlink(current); err == nil {
		_ = os.Remove(previous)
		if err = os.Symlink(link, previous); err != nil {
			return err
		}
	}
	_ = os.Remove(current)
	if err := os.Symlink(filepath.Join("releases", version), current); err != nil {
		return err
	}
	if restart != nil {
		if err := restart(); err != nil {
			return errors.New("release_restart_failed")
		}
	}
	return nil
}

func DecodeReleasePayload(payload map[string]any) (version string, gate, manage []byte, gateSum, manageSum string, err error) {
	for key := range payload {
		switch key {
		case "version", "gate_sha256", "manage_sha256", "gate_body", "manage_body":
		default:
			return "", nil, nil, "", "", errors.New("release_payload_refused")
		}
	}
	version = fmtText(payload["version"])
	gateSum = fmtText(payload["gate_sha256"])
	manageSum = fmtText(payload["manage_sha256"])
	gate, err = decodeBody(fmtText(payload["gate_body"]))
	if err != nil {
		return "", nil, nil, "", "", err
	}
	manage, err = decodeBody(fmtText(payload["manage_body"]))
	if err != nil {
		return "", nil, nil, "", "", err
	}
	return version, gate, manage, gateSum, manageSum, nil
}

func decodeBody(value string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(data) == 0 || len(data) > 32*1024*1024 {
		return nil, errors.New("release_body_refused")
	}
	return data, nil
}

func matchSum(data []byte, sum string) error {
	digest := sha256.Sum256(data)
	if !bytes.Equal([]byte(sum), []byte(hex.EncodeToString(digest[:]))) {
		return errors.New("release_checksum_mismatch")
	}
	return nil
}

func writeExecutable(path string, data []byte) error {
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, 0755); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func sameRelease(current, releaseDir, gatePath string, gate []byte) bool {
	link, err := os.Readlink(current)
	if err != nil || filepath.Clean(link) != filepath.Clean(filepath.Join("releases", filepath.Base(releaseDir))) {
		return false
	}
	existing, err := os.ReadFile(gatePath)
	return err == nil && bytes.Equal(existing, gate)
}

func releaseReplaceHandler(root string, restart func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var payload map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 70<<20)).Decode(&payload) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		version, gate, manage, gateSum, manageSum, err := DecodeReleasePayload(payload)
		if err != nil {
			writeReleaseError(w, err)
			return
		}
		if err = InstallRelease(root, version, gate, manage, gateSum, manageSum, restart); err != nil {
			writeReleaseError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"applied":true}`))
	}
}

func writeReleaseError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(422)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
}
