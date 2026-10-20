package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

type diagnosticRunner struct {
	calls []string
	fail  bool
}

func (r *diagnosticRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if r.fail {
		return errors.New("secret fixture validation message")
	}
	return nil
}
func (r *diagnosticRunner) RunOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return []byte(`{"MESSAGE":"TLS failed for secret-password@example.test","PRIORITY":"3","__REALTIME_TIMESTAMP":"1760000000000000"}`), nil
}

func TestCoreDiagnosticProjectionAndFixedCommands(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"inbounds":[{"password":"secret-password","settings":{"privateKey":"secret-key"}}],"outbounds":[{"server":"secret-host"}],"route":{"rules":[{}]},"future_secret":"secret-future"}`)
	for _, core := range []string{"xray", "sing-box"} {
		if err := os.WriteFile(filepath.Join(dir, core+".json"), body, 0600); err != nil {
			t.Fatal(err)
		}
		r := &diagnosticRunner{fail: true}
		out := coreDiagnostics(context.Background(), core, dir, dir, r)
		encoded, _ := json.Marshal(out)
		if strings.Contains(string(encoded), "secret-") || len(r.calls) != 2 || !strings.Contains(r.calls[1], "--unit=migate-gate-"+core+".service") {
			t.Fatal("diagnostic leaked or command scope changed")
		}
		c := out["config"].(map[string]any)
		if c["rules"] != 1 || c["inbounds"] != 1 || c["validation"] != "failed_or_unavailable" {
			t.Fatal("incorrect preview")
		}
		if core == "xray" && !strings.Contains(r.calls[0], "run -test -config") || core == "sing-box" && !strings.Contains(r.calls[0], "check -c") {
			t.Fatal("unsafe validation command")
		}
	}
	for _, raw := range []string{`null`, `[]`, `garbage`} {
		if _, err := summarizeCoreConfig([]byte(raw)); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	for _, raw := range []string{`bad journal`, `[output truncated]`} {
		if classifyCoreJournal([]byte(raw), nil)["validity"] != "unknown" {
			t.Fatal("malformed/truncated journal fabricated")
		}
	}
	if classifyCoreJournal(nil, errors.New("secret"))["validity"] != "unknown" {
		t.Fatal("journal failure as zero")
	}
	if classifyCoreJournal(nil, nil)["sampled"] != 0 {
		t.Fatal("empty successful journal incorrect")
	}
	binary := classifyCoreJournal([]byte(`{"MESSAGE":[0,255,1],"PRIORITY":"6"}`), nil)
	if binary["validity"] != "fresh" || binary["categories"].(map[string]int)["binary"] != 1 {
		t.Fatal("journald binary message not counted separately")
	}
}

func TestCoreInspectReadOnlyReceiptAndBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux helper Unix socket")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "helper.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	inspections, applies := 0, 0
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/core-diagnostics" {
			applies++
			w.WriteHeader(500)
			return
		}
		inspections++
		json.NewEncoder(w).Encode(map[string]any{"core": "xray", "read_only": true})
	})}
	go server.Serve(listener)
	defer server.Close()
	a, err := New(Config{DeviceID: 8, DataDir: filepath.Join(dir, "data"), Socket: socket})
	if err != nil {
		t.Fatal(err)
	}
	defer a.DB.Close()
	a.State.ApplyPending = true
	before, _ := json.Marshal(a.State)
	task := contracts.Task{ID: 11, DeviceID: 8, Revision: 1, Operation: "core.inspect", ExpiresAt: time.Now().Add(time.Minute), Payload: map[string]any{"core": "xray"}}
	for i := 0; i < 2; i++ {
		if a.execute(context.Background(), task).Status != "applied" {
			t.Fatal("inspection failed")
		}
	}
	after, _ := json.Marshal(a.State)
	if !reflect.DeepEqual(before, after) || inspections != 1 || applies != 0 {
		t.Fatal("inspection mutated core or replayed helper")
	}
	for _, payload := range []map[string]any{{"core": "x-ui"}, {"core": "../xray"}, {"core": "xray", "command": "restart"}} {
		task.ID++
		task.Payload = payload
		if a.execute(context.Background(), task).Status != "failed" {
			t.Fatal("unsafe inspect accepted")
		}
	}
	task.ID++
	task.Payload = map[string]any{"core": "xray"}
	task.DeviceID = 9
	if a.execute(context.Background(), task).Status != "failed" || inspections != 1 || applies != 0 {
		t.Fatal("cross-device inspection executed")
	}
	task.DeviceID = 8
	task.ExpiresAt = time.Now().Add(-time.Minute)
	if a.execute(context.Background(), task).Status != "failed" || inspections != 1 {
		t.Fatal("expired inspection executed")
	}
}
