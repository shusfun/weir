package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	runtimecmd "github.com/imzyb/MiGate/internal/runtime/command"
)

// This preview deliberately projects only structural counts. Never recursively
// redact and return raw configs: new protocol fields can contain new secrets.
func summarizeCoreConfig(body []byte) (map[string]any, error) {
	var config map[string]json.RawMessage
	if json.Unmarshal(body, &config) != nil || config == nil {
		return nil, errors.New("invalid_config_json")
	}
	count := func(raw json.RawMessage) int {
		var rows []json.RawMessage
		_ = json.Unmarshal(raw, &rows)
		return len(rows)
	}
	var routing map[string]json.RawMessage
	_ = json.Unmarshal(config["routing"], &routing)
	if routing == nil {
		_ = json.Unmarshal(config["route"], &routing)
	}
	sum := sha256.Sum256(body)
	return map[string]any{"sha256": hex.EncodeToString(sum[:]), "bytes": len(body), "inbounds": count(config["inbounds"]), "outbounds": count(config["outbounds"]), "rules": count(routing["rules"])}, nil
}

// Journal MESSAGE is classified locally and discarded. Neither addresses,
// credentials nor arbitrary log fragments cross the helper boundary.
func classifyCoreJournal(body []byte, commandErr error) map[string]any {
	unknown := map[string]any{"validity": "unknown", "reason": "journal_unavailable_or_truncated", "window_minutes": 15, "limit": 200}
	if commandErr != nil || bytes.Contains(body, []byte("[output truncated]")) {
		return unknown
	}
	counts := map[string]int{}
	errorsCount, total := 0, 0
	var last time.Time
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Message   json.RawMessage `json:"MESSAGE"`
			Priority  string          `json:"PRIORITY"`
			Timestamp string          `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal(line, &entry) != nil || total >= 200 {
			return unknown
		}
		category := "other"
		var message string
		if json.Unmarshal(entry.Message, &message) != nil {
			// Journald encodes binary messages as byte arrays. Count but do not
			// decode or classify binary content as readable text.
			var binary []json.RawMessage
			if json.Unmarshal(entry.Message, &binary) != nil || binary == nil {
				return unknown
			}
			category = "binary"
		}
		text := strings.ToLower(message)
		switch {
		case strings.Contains(text, "address already in use"), strings.Contains(text, "bind:"):
			category = "listener"
		case strings.Contains(text, "tls"), strings.Contains(text, "certificate"), strings.Contains(text, "reality"):
			category = "tls"
		case strings.Contains(text, "auth"), strings.Contains(text, "invalid user"), strings.Contains(text, "invalid password"):
			category = "authentication"
		case strings.Contains(text, "dns"), strings.Contains(text, "lookup"):
			category = "dns"
		case strings.Contains(text, "config"), strings.Contains(text, "parse"):
			category = "configuration"
		case strings.Contains(text, "timeout"), strings.Contains(text, "connection refused"), strings.Contains(text, "unreachable"):
			category = "network"
		}
		counts[category]++
		if priority, err := strconv.Atoi(entry.Priority); err == nil && priority >= 0 && priority <= 3 {
			errorsCount++
		}
		if micros, err := strconv.ParseInt(entry.Timestamp, 10, 64); err == nil && micros > 0 {
			when := time.UnixMicro(micros)
			if when.After(last) {
				last = when
			}
		}
		total++
	}
	out := map[string]any{"validity": "fresh", "window_minutes": 15, "limit": 200, "sampled": total, "errors": errorsCount, "categories": counts, "possibly_limited": total == 200}
	if !last.IsZero() {
		out["latest_at"] = last.UTC().Format(time.RFC3339)
	}
	return out
}

func coreDiagnostics(ctx context.Context, core, configDir, binaryDir string, runner runtimecmd.CommandRunner) map[string]any {
	out := map[string]any{"core": core, "observed_at": time.Now().UTC().Format(time.RFC3339), "source": "gate-helper", "validity": "fresh", "read_only": true}
	config := map[string]any{"validity": "unknown", "reason": "config_unavailable"}
	path := filepath.Join(configDir, core+".json")
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		info, statErr := f.Stat()
		if statErr == nil && info.Mode().IsRegular() && info.Size() <= 2<<20 {
			if body, err := io.ReadAll(io.LimitReader(f, (2<<20)+1)); err == nil && len(body) <= 2<<20 {
				if summary, err := summarizeCoreConfig(body); err == nil {
					config = summary
					config["validity"] = "fresh"
					config["modified_at"] = info.ModTime().UTC().Format(time.RFC3339)
					args := []string{"run", "-test", "-config", path}
					if core == "sing-box" {
						args = []string{"check", "-c", path}
					}
					if err := runner.Run(ctx, filepath.Join(binaryDir, core), args...); err != nil {
						config["validation"] = "failed_or_unavailable"
					} else {
						config["validation"] = "passed"
					}
				}
			}
		}
	}
	out["config"] = config
	log, err := runner.RunOutput(ctx, "journalctl", "--unit=migate-gate-"+core+".service", "--since=-15min", "--lines=200", "--output=json", "--output-fields=MESSAGE,PRIORITY,__REALTIME_TIMESTAMP", "--no-pager", "--quiet")
	out["journal"] = classifyCoreJournal(log, err)
	return out
}

func (a *Agent) readCoreDiagnostics(ctx context.Context, core string) (map[string]any, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 18 * time.Second}
	body, _ := json.Marshal(map[string]string{"core": core})
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/core-diagnostics", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("core_diagnostics_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("core_diagnostics_unavailable")
	}
	var result map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&result); err != nil {
		return nil, errors.New("core_diagnostics_invalid_response")
	}
	return result, nil
}
