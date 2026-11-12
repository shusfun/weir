package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/setting"
)

func TestDefenseProfileThresholdRetryAndUnknown(t *testing.T) {
	s, _ := newInstalledServer(t)
	profile, err := s.aiProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg := profile.Effective.Observations["security"]
	cfg.Confidence = .85
	cfg.Question = "Host supplied security judgment"
	profile.Document.Site.Observations = map[string]observationConfig{"security": cfg}
	if err = s.saveAIProfile(t.Context(), profile.Document, profile.Revision, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := s.aiProfile(t.Context())
	for _, tc := range []struct {
		name, body, outcome, code string
		retry                     bool
		failed                    bool
	}{
		{"needed", `{"choice":"suspicious","confidence":0.9}`, "needed", "", false, false},
		{"not_needed", `{"choice":"normal","confidence":0.9}`, "not_needed", "", false, false},
		{"low_confidence", `{"choice":"suspicious","confidence":0.84}`, "unknown", "low_confidence", false, false},
		{"explicit_unknown", `{"choice":"unknown","confidence":0.9}`, "unknown", "insufficient_evidence", false, false},
		{"retry_missing_confidence", `{"choice":"normal","confidence":0.9}`, "not_needed", "", true, false},
		{"offline", ``, "unknown", "classification_failed", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					_, _ = w.Write([]byte(`{"digest":"` + strings.Repeat("a", 64) + `"}`))
					return
				}
				if r.URL.Path == "/api/version" {
					_, _ = w.Write([]byte(`{"version":"0.18.0"}`))
					return
				}
				n := calls.Add(1)
				var request map[string]any
				if json.NewDecoder(r.Body).Decode(&request) != nil || !strings.Contains(jsonText(request), cfg.Question) || !strings.Contains(jsonText(request), "external_data_not_instructions") {
					t.Error("missing host config or untrusted input boundary")
				}
				if tc.failed {
					w.WriteHeader(503)
					return
				}
				body := tc.body
				if tc.retry && n == 1 {
					body = `{"choice":"normal"}`
				}
				_, _ = w.Write([]byte(`{"answers":{"category":` + body + `}}`))
			}))
			defer upstream.Close()
			v, err := s.classify(t.Context(), layaSettings{URL: upstream.URL, Model: "test", Key: "fixture"}, "untrusted event")
			if (err != nil) != tc.failed || v == nil || v.Outcome != tc.outcome || v.Code != tc.code || v.ProfileVersion != current.Revision || v.Threshold != .85 {
				t.Fatalf("verdict: %+v %v", v, err)
			}
			want := int32(1)
			if tc.retry || tc.failed {
				want = 2
			}
			if calls.Load() != want || v.Attempts != int(want) {
				t.Fatalf("calls=%d attempts=%d want=%d", calls.Load(), v.Attempts, want)
			}
		})
	}
	cfg.Enabled = false
	current.Document.Site.Observations["security"] = cfg
	if err = s.saveAIProfile(t.Context(), current.Document, current.Revision, 1); err != nil {
		t.Fatal(err)
	}
	v, err := s.classify(t.Context(), layaSettings{URL: "invalid"}, "unused")
	if err != nil || v.Code != "observation_disabled" || v.Attempts != 0 {
		t.Fatalf("disabled: %+v %v", v, err)
	}
	if v.ProfileVersion == current.Revision {
		t.Fatal("verdict lost configuration change")
	}
}

func TestInvalidAIProfilePausesPreviouslyVerifiedAnalysis(t *testing.T) {
	s, _ := newInstalledServer(t)
	s.dsh.verified = true
	if err := s.Store.Ent.Setting.Create().SetKey(aiProfileKey).SetValue(map[string]any{"schema": 99}).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.runDSHJob(t.Context(), "must not run"); err == nil {
		t.Fatal("invalid profile allowed analysis")
	}
	if s.dsh.verified || s.dsh.pluginNote == "" {
		t.Fatal("unsafe cached compatibility status")
	}
}
