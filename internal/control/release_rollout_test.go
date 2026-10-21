package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReleaseRequestRequiresTagAndRewritesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIGATE_RELEASE_STATE_DIR", dir)
	gin.SetMode(gin.TestMode)
	server := &Server{}
	post := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/release", strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		server.requestRelease(context)
		return recorder
	}
	if got := post(`{"confirm":true}`); got.Code != 400 {
		t.Fatalf("empty tag status %d", got.Code)
	}
	if got := post(`{"confirm":true,"tag":"latest"}`); got.Code != 400 {
		t.Fatalf("invalid tag status %d", got.Code)
	}
	first := post(`{"confirm":true,"tag":"v0.1.6"}`)
	if first.Code != 200 {
		t.Fatalf("first request %d %s", first.Code, first.Body.String())
	}
	requestPath := filepath.Join(dir, "release-request.json")
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]string
	if json.Unmarshal(raw, &request) != nil || request["tag"] != "v0.1.6" {
		t.Fatalf("request file %#v", request)
	}
	second := post(`{"confirm":true,"tag":"v0.1.7"}`)
	if second.Code != 200 {
		t.Fatalf("second request %d %s", second.Code, second.Body.String())
	}
	raw, err = os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	request = map[string]string{}
	if json.Unmarshal(raw, &request) != nil || request["tag"] != "v0.1.7" {
		t.Fatalf("rewritten request %#v", request)
	}
	stage, detail, pending := server.releaseJob()
	if stage != "requested" || detail == "" || !pending {
		t.Fatalf("job %s %s pending=%v", stage, detail, pending)
	}
}

func TestReleaseJobHidesPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIGATE_RELEASE_STATE_DIR", dir)
	body := []byte(`{"status":"failed","message":"失败于 /var/lib/migate-control/secret"}`)
	if err := os.WriteFile(filepath.Join(dir, "release-status.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	stage, detail, pending := (&Server{}).releaseJob()
	if stage != "failed" || strings.Contains(detail, "/") || pending {
		t.Fatalf("job %s %s pending=%v", stage, detail, pending)
	}
}
