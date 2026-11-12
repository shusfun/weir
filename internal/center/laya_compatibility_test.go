package center

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shusfun/weir/packages/laya"
)

func TestLayaCompatibilityLayersAndUnknown(t *testing.T) {
	doc := aiProfileDocument{Schema: 1, External: aiProfileLayer{Schema: 1}, Site: aiProfileLayer{Schema: 1}}
	doc.External.LayaCompatibility = &layaCompatibilityConfig{VerifiedRange: ">=0.18.0 <0.20.0", OutOfRange: laya.DegradeReject}
	doc.Site.LayaCompatibility = &layaCompatibilityConfig{VerifiedRange: "=0.19.0", OutOfRange: laya.DegradeBaseOnly}
	effective, sources, err := resolveAIProfile(doc)
	if err != nil || effective.LayaCompatibility.VerifiedRange != "=0.19.0" || sources["laya_compatibility"] != "site" {
		t.Fatalf("layers: %+v %v", effective, err)
	}
	for _, tc := range []struct {
		version          string
		base, background bool
		code             string
	}{
		{"0.19.0", true, true, ""}, {"0.18.0", true, false, "laya_version_out_of_range"}, {"", false, false, "laya_runtime_version_unknown"}, {"invalid", false, false, "laya_runtime_version_unknown"},
	} {
		got := resolveLayaCompatibility(*effective.LayaCompatibility, tc.version)
		if got.EnableBase != tc.base || got.EnableBackground != tc.background || got.Code != tc.code {
			t.Fatalf("version %q: %+v", tc.version, got)
		}
	}
	doc.Site.LayaCompatibility.OutOfRange = "silently_allow"
	if _, _, err := resolveAIProfile(doc); err == nil {
		t.Fatal("unknown degradation policy accepted")
	}
}

func TestLayaOutOfRangeKeepsBasicDecisionButPausesAutomaticAnalysis(t *testing.T) {
	s, _ := newInstalledServer(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			_, _ = w.Write([]byte(`{"digest":"` + laya.ModelManifestSHA256 + `"}`))
			return
		}
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte(`{"version":"0.19.0"}`))
			return
		}
		_, _ = w.Write([]byte(`{"answers":{"category":{"choice":"suspicious","confidence":0.99}}}`))
	}))
	defer srv.Close()
	v, err := s.classify(t.Context(), layaSettings{URL: srv.URL, Model: "fixture"}, "diagnostic text")
	if err != nil || v.Outcome != "needed" || v.RuntimeVersion != "0.19.0" || v.BackgroundAllowed == nil || *v.BackgroundAllowed || v.Compatibility.Code != "laya_version_out_of_range" {
		t.Fatalf("decision: %+v err=%v", v, err)
	}
	profile, _ := s.aiProfile(t.Context())
	profile.Document.Site.LayaCompatibility = &layaCompatibilityConfig{VerifiedRange: "=0.18.0", OutOfRange: laya.DegradeReject}
	if err := s.saveAIProfile(t.Context(), profile.Document, profile.Revision, 1); err != nil {
		t.Fatal(err)
	}
	v, err = s.classify(t.Context(), layaSettings{URL: srv.URL, Model: "fixture"}, "diagnostic text")
	if err == nil || v.Outcome != "unknown" || v.Attempts != 0 || v.Code != "laya_version_out_of_range" {
		t.Fatalf("reject: %+v err=%v", v, err)
	}
}
