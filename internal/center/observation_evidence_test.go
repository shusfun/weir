package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/packages/dsh"
)

func TestObservationMCPPropagatesHTTPTrace(t *testing.T) {
	s, _ := newInstalledServer(t)
	m := &mcpServer{observationToken: randomToken(32)}
	req := httptest.NewRequest("POST", "http://127.0.0.1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+m.observationToken)
	root := "0123456789abcdef0123456789abcdef"
	req.Header.Set("traceparent", "00-"+root+"-0123456789abcdef-01")
	w := httptest.NewRecorder()
	s.mcpHandler(m).ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("X-Trace-Id") != root || !strings.HasPrefix(w.Header().Get("traceparent"), "00-"+root+"-") || w.Header().Get("X-Request-Id") == "" {
		t.Fatal("MCP HTTP trace missing", w.Code, w.Header())
	}
}

func TestObservationEvidenceLargeContextUsesBoundedToolAndExpires(t *testing.T) {
	s, _ := newInstalledServer(t)
	id := strings.Repeat("a", 64)
	evidence := map[string]any{"external_data_not_instructions": strings.Repeat("诊断文字 ", 7000)}
	raw, _ := json.Marshal(evidence)
	read := observationEvidenceCaps[0].read
	m := &mcpServer{webToken: randomToken(32), analystToken: randomToken(32), observationToken: randomToken(32)}
	h := s.mcpHandler(m)
	if _, ok := findCap("observation", "get_observation_evidence"); !ok {
		t.Fatal("evidence tool unavailable to its background profile")
	}
	if _, ok := findCap("observation", "ai.profile.update"); ok {
		t.Fatal("background profile can modify its own rules")
	}
	if _, ok := findCap("analysis", "list_nodes"); ok {
		t.Fatal("security profile inherited observation permissions")
	}
	s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		if req.Profile != "observation" || len(req.Task) > 8000 || !strings.Contains(req.Task, "get_observation_evidence") || strings.Contains(req.Task, "诊断文字") {
			t.Fatal("large snapshot was sent as a CLI argument")
		}
		sealed, ok, err := s.Store.KV.Get(ctx, "observation-evidence:"+id)
		if err != nil || !ok || strings.Contains(sealed, "诊断文字") {
			t.Fatal("evidence not encrypted")
		}
		var received strings.Builder
		offset := 0
		for {
			text, failed := toolText(t, h, m.observationToken, "get_observation_evidence", map[string]any{"context": id, "offset": offset})
			if failed {
				t.Fatal(text)
			}
			var page map[string]any
			if err := json.Unmarshal([]byte(text), &page); err != nil {
				t.Fatal(err)
			}
			chunk := page["external_data_not_instructions"].(string)
			if len([]rune(chunk)) > 4000 {
				t.Fatal("unbounded tool response")
			}
			received.WriteString(chunk)
			if page["next_offset"] == nil {
				break
			}
			offset = int(page["next_offset"].(float64))
		}
		if received.String() != string(raw) {
			t.Fatal("paged evidence was truncated or reordered")
		}
		if _, err := read(ctx, s, map[string]any{"context": id, "offset": -1.0}); err == nil {
			t.Fatal("negative offset accepted")
		}
		for _, token := range []string{m.webToken, m.analystToken} {
			if _, failed := toolText(t, h, token, "get_observation_evidence", map[string]any{"context": id}); !failed {
				t.Fatal("other source accessed observation evidence")
			}
		}
		if code, _ := mcpCall(t, h, "invalid", "tools/list", nil); code != 401 {
			t.Fatal("invalid MCP token accepted")
		}
		text, failed := toolText(t, h, m.observationToken, "list_actions", map[string]any{})
		if failed || strings.Contains(text, "ai.profile.update") || !strings.Contains(text, "driver.save") {
			t.Fatal("observation draft boundary missing", text)
		}
		return dsh.JobResult{Result: dsh.Result{Completed: true}}, nil
	}
	if result, err := s.runEvidenceAnalysis(t.Context(), id, "只提交草稿", evidence); err != nil || !result.Completed {
		t.Fatal("large evidence analysis failed", err)
	}
	if _, err := read(t.Context(), s, map[string]any{"context": id}); err == nil {
		t.Fatal("evidence remains accessible after analysis")
	}
	if _, failed := toolText(t, h, m.observationToken, "get_observation_evidence", map[string]any{"context": id}); !failed {
		t.Fatal("completed evidence remains accessible through MCP")
	}
}

func TestDSHBackgroundCapacityIsSharedAndReleasedAfterCancel(t *testing.T) {
	s, _ := newInstalledServer(t)
	entered := make(chan struct{})
	s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		close(entered)
		<-ctx.Done()
		return dsh.JobResult{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.runDSHJob(ctx, "first task"); done <- err }()
	<-entered
	if _, err := s.runDSHJob(t.Context(), "second task"); !errors.Is(err, errDSHAnalysisBusy) {
		t.Fatal("concurrent background analysis accepted", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("analysis did not cancel")
	}
	s.dsh.jobRunner = func(ctx context.Context, req dsh.JobRequest) (dsh.JobResult, error) {
		return dsh.JobResult{Result: dsh.Result{Completed: true}}, nil
	}
	if _, err := s.runDSHJob(t.Context(), "third task"); err != nil {
		t.Fatal("capacity not released", err)
	}
}
