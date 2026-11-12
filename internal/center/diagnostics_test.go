package center

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/diagnostic"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func TestDiagnosticPreviewExportFreezesRedactedSnapshotAndAudits(t *testing.T) {
	s, admin := newInstalledServer(t)
	logs := captureLog(t)
	ctx := t.Context()
	source, err := s.Store.Ent.SubSource.Create().SetName("测试源 password=NAMESECRET").SetURL("https://u:URLSECRET@up.example/sub/SECRET?token=SECRET").SetLastError("upstream https://up.example/APISECRET?token=QUERYSECRET password=ERRORSECRET").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parent := "00-0123456789abcdef0123456789abcdef-1111111111111111-01"
	w := admin.do("POST", "/api/v1/admin/workers/diagnostics/preview", map[string]any{"items": []diagnostic.Selection{{Kind: "source", ID: strconv.Itoa(source.ID)}, {Kind: "center", ID: "current"}}}, map[string]string{"traceparent": parent})
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	var preview struct {
		ID     string            `json:"id"`
		Bundle diagnostic.Bundle `json:"bundle"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &preview); err != nil || preview.Bundle.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("preview trace: %+v %v", preview, err)
	}
	for _, secret := range []string{"NAMESECRET", "URLSECRET", "APISECRET", "QUERYSECRET", "ERRORSECRET"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("preview leaked %s", secret)
		}
	}
	if _, err = source.Update().SetName("CHANGEDAFTERPREVIEW").Save(ctx); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/workers/diagnostics/" + preview.ID + "/export"
	key := "source:" + strconv.Itoa(source.ID)
	w = admin.do("POST", path, map[string]any{"keep": []string{key}}, map[string]string{"traceparent": parent})
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("export: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(z.File) != 2 {
		t.Fatalf("zip: %v", err)
	}
	r, _ := z.File[0].Open()
	raw, _ := io.ReadAll(r)
	r.Close()
	var bundle diagnostic.Bundle
	if err = json.Unmarshal(raw, &bundle); err != nil || len(bundle.Entries) != 1 || bundle.Entries[0].Selection.Key() != key || bytes.Contains(raw, []byte("CHANGEDAFTERPREVIEW")) {
		t.Fatalf("snapshot changed or excluded entry remained: %s %v", raw, err)
	}
	row, err := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("diagnostic.export"), audit.TargetEQ(preview.ID)).Only(ctx)
	if err != nil || row.Result != "prepared" || row.Detail["trace_id"] != preview.Bundle.TraceID || row.Detail["snapshot_trace_id"] != preview.Bundle.TraceID || len(row.Detail["sha256"].(string)) != 64 {
		t.Fatalf("export audit: %+v %v", row, err)
	}
	if !strings.Contains(logs.String(), "trace="+preview.Bundle.TraceID) || !strings.Contains(logs.String(), "observation="+preview.ID) {
		t.Fatalf("export log did not correlate: %s", logs.String())
	}
	if w = admin.do("POST", path, map[string]any{"keep": []string{key, "source:999999"}}, nil); w.Code != 400 {
		t.Fatalf("unpreviewed entry accepted: %d", w.Code)
	}
	// A shared store is sufficient to resume an immutable preview on another center.
	other := &Server{Config: s.Config, Store: s.Store, Crypto: s.Crypto, Path: s.Path}
	admin.router = other.Router()
	if w = admin.do("POST", path, map[string]any{"keep": []string{key}}, nil); w.Code != 200 {
		t.Fatalf("preview relied on process memory: %d %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticPreviewExportRejectsInvalidExpiredAndUnauthorized(t *testing.T) {
	s, admin := newInstalledServer(t)
	for _, items := range [][]diagnostic.Selection{nil, {{Kind: "shell", ID: "current"}}, {{Kind: "source", ID: "-1"}}, {{Kind: "task", ID: "secret"}}, {{Kind: "center", ID: "current"}, {Kind: "center", ID: "current"}}} {
		if w := admin.do("POST", "/api/v1/admin/workers/diagnostics/preview", map[string]any{"items": items}, nil); w.Code != 400 {
			t.Fatalf("invalid selection: %d %s", w.Code, w.Body.String())
		}
	}
	if w := admin.do("POST", "/api/v1/admin/workers/diagnostics/preview", map[string]any{"items": []diagnostic.Selection{{Kind: "source", ID: "999999"}}}, nil); w.Code != 404 {
		t.Fatalf("missing source: %d", w.Code)
	}
	w := admin.do("POST", "/api/v1/admin/workers/diagnostics/preview", map[string]any{"items": []diagnostic.Selection{{Kind: "center", ID: "current"}}}, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := decode(t, w)["id"].(string)
	key := "diagnostic-preview:" + id
	sealed, _, _ := s.Store.KV.Get(t.Context(), key)
	raw, _ := s.Crypto.Decrypt(sealed)
	var p diagnosticPreview
	_ = json.Unmarshal([]byte(raw), &p)
	p.Actor++
	rawBytes, _ := json.Marshal(p)
	sealed, _ = s.Crypto.Encrypt(string(rawBytes))
	_ = s.Store.KV.Set(t.Context(), key, sealed, diagnosticTTL)
	path := "/api/v1/admin/workers/diagnostics/" + id + "/export"
	if w := admin.do("POST", path, map[string]any{"keep": []string{"center:current"}}, nil); w.Code != 404 {
		t.Fatalf("other administrator's snapshot accepted: %d", w.Code)
	}
	_ = s.Store.KV.Del(t.Context(), key)
	if w := admin.do("POST", path, map[string]any{"keep": []string{"center:current"}}, nil); w.Code != 404 {
		t.Fatalf("expired snapshot accepted: %d", w.Code)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "diagnostic-user@example.test", "password": "Passw0rd"}, 200)
	u := login(t, s, "diagnostic-user@example.test", "Passw0rd")
	anon := &testClient{t: t, router: s.Router()}
	for _, path := range []string{"/api/v1/admin/workers/diagnostics/preview", path} {
		if w := u.do("POST", path, nil, nil); w.Code != 403 {
			t.Fatalf("ordinary user: %d", w.Code)
		}
		if w := anon.do("POST", path, nil, nil); w.Code != 401 {
			t.Fatalf("anonymous: %d", w.Code)
		}
	}
}

func TestDiagnosticTaskSnapshotIncludesCollectedTimelineWithoutSecrets(t *testing.T) {
	s, _ := newInstalledServer(t)
	ctx := t.Context()
	traceID := strings.Repeat("a", 32)
	raw := json.RawMessage(`{"credentials":{"url":"https://up.example/sub/INPUTSECRET"},"user_agent":"test","max_bytes":1024}`)
	sealed, _ := s.Crypto.Encrypt(string(raw))
	request := taskqueue.Request{Kind: contracts.TaskSubscriptionFetch, TraceID: traceID, Key: "task-secret-key", Payload: raw, Candidates: []int{1}, Deadline: time.Now().Add(time.Minute)}
	record, err := s.Store.Tasks.Enqueue(ctx, request, sealed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.ExecContext(ctx, `UPDATE worker_tasks SET gate_id=1 WHERE id=$1`, record.Task.ID); err != nil {
		t.Fatal(err)
	}
	e, err := s.diagnosticEntry(ctx, diagnostic.Selection{Kind: "task", ID: record.Task.ID})
	if err != nil || !strings.Contains(strings.Join(e.Limitations, " "), "明细未取得") {
		t.Fatalf("missing evidence not labeled: %+v %v", e, err)
	}
	// Store an already validated, encrypted read result just as the result endpoint does.
	stages := contracts.TraceReadResult{Records: []contracts.TaskStageRecord{{TaskID: record.Task.ID, TraceID: traceID, Kind: request.Kind, Attempt: 1, Stage: "network", Origin: "gate.executeNetworkTask", Outcome: "failed", ErrorCode: "network_failed", Started: time.Now().Add(-time.Second), Finished: time.Now()}}, CollectedAt: time.Now(), Complete: false, MissingReason: "bounded_local_history"}
	result, _ := json.Marshal(stages)
	resultSealed, _ := s.Crypto.Encrypt(string(result))
	resultRaw, _ := json.Marshal(map[string]string{"sealed": resultSealed})
	traceReq, _ := json.Marshal(contracts.TraceReadTask{TraceID: traceID, Limit: 200})
	traceSealed, _ := s.Crypto.Encrypt(string(traceReq))
	traceRecord, err := s.Store.Tasks.Enqueue(ctx, taskqueue.Request{Kind: contracts.TaskTraceRead, TraceID: traceID, Key: "trace-result", Payload: traceReq, Requirements: contracts.TaskRequirements{TargetGateID: 1}, Candidates: []int{1}, Deadline: time.Now().Add(time.Minute)}, traceSealed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.DB.ExecContext(ctx, `UPDATE worker_tasks SET gate_id=1,status='succeeded',result=$1 WHERE id=$2`, string(resultRaw), traceRecord.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	e, err = s.diagnosticEntry(ctx, diagnostic.Selection{Kind: "task", ID: record.Task.ID})
	data, _ := json.Marshal(e)
	if err != nil || !bytes.Contains(data, []byte("gate_timeline")) || !bytes.Contains(data, []byte("gate.executeNetworkTask")) || bytes.Contains(data, []byte("INPUTSECRET")) || bytes.Contains(data, []byte("task-secret-key")) {
		t.Fatalf("invalid evidence snapshot: %s %v", data, err)
	}
}

func TestDiagnosticSourceLocationsResolveToFunctions(t *testing.T) {
	for origin, location := range diagnosticSources {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", location.File), nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", origin, err)
		}
		found := false
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == location.Function {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s points to absent function %s", origin, location.Function)
		}
	}
}
