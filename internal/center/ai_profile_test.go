package center

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/store/ent/resource"
)

func TestAIProfileLayersAndRegisteredBoundaries(t *testing.T) {
	s, _ := newInstalledServer(t)
	v, err := s.aiProfile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Effective.Observations["security"].Confidence != 0.6 || len(v.Effective.Skills) < 4 {
		t.Fatalf("missing defaults: %+v", v)
	}
	external, site := "external instructions", "site instructions"
	v.Document.External.Instructions = &external
	v.Document.Site.Instructions = &site
	cfg := v.Effective.Observations["security"]
	cfg.Confidence = .81
	v.Document.External.Observations = map[string]observationConfig{"security": cfg}
	cfg.Confidence = .92
	v.Document.Site.Observations = map[string]observationConfig{"security": cfg}
	effective, sources, err := resolveAIProfile(v.Document)
	if err != nil || *effective.Instructions != site || effective.Observations["security"].Confidence != .92 || sources["observations/security"] != "site" {
		t.Fatalf("layer precedence: %+v %+v %v", effective, sources, err)
	}
	v.Document.Site.Observations["unknown.collector"] = cfg
	if _, _, err = resolveAIProfile(v.Document); err == nil {
		t.Fatal("configuration added collector")
	}
	delete(v.Document.Site.Observations, "unknown.collector")
	spam := v.Effective.Observations["content.spam"]
	spam.Skill = "weir-ops"
	v.Document.Site.Observations["content.spam"] = spam
	if _, _, err = resolveAIProfile(v.Document); err == nil {
		t.Fatal("content moderation routed to dsh")
	}
	v.Document.Site.Observations = nil
	v.Document.Site.Skills = map[string]string{"host-review/SKILL.md": "Host review instructions"}
	cfg.Skill = "host-review"
	v.Document.Site.Observations = map[string]observationConfig{"subscription.health": cfg}
	if _, _, err = resolveAIProfile(v.Document); err != nil {
		t.Fatalf("host skill mapping rejected: %v", err)
	}
	delete(v.Document.Site.Skills, "host-review/SKILL.md")
	if _, _, err = resolveAIProfile(v.Document); err == nil {
		t.Fatal("missing skill accepted")
	}
	v.Document.Site.Observations = nil
	v.Document.Site.Skills = map[string]string{"../escape/SKILL.md": "x"}
	if _, _, err = resolveAIProfile(v.Document); err == nil {
		t.Fatal("skill escaped root")
	}
}

func TestAIProfileDraftApprovalHistoryAndRollback(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := t.Context()
	before, err := s.aiProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	doc := before.Document
	instructions := "# Host instructions\nRequire explicit approval for every configuration change."
	doc.Site.Instructions = &instructions
	doc.Site.Skills = map[string]string{"weir-security/SKILL.md": "---\nname: weir-security\n---\nUpdated host security procedure"}
	doc.Site.Plugin = builtinPluginFiles()
	doc.Site.Plugin["package.json"] = strings.Replace(doc.Site.Plugin["package.json"], "<0.3.0-0", "<0.4.0-0", 1)
	text, _ := json.MarshalIndent(doc, "", "  ")
	w := admin.do("POST", "/api/v1/admin/ai/profile/draft", map[string]any{"text": string(text)}, nil)
	if w.Code != 201 {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	var draft Draft
	if err = json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	still, _ := s.aiProfile(ctx)
	if still.Revision != before.Revision || len(draft.Steps) != 1 || len(draft.Steps[0].Diff) == 0 {
		t.Fatal("draft changed live configuration or omitted diff")
	}
	for _, line := range draft.Steps[0].Diff {
		if line.Op == "…" {
			t.Fatal("self modification diff folded")
		}
	}
	if _, ok := findCap("analysis", "ai.profile.update"); ok {
		t.Fatal("background security profile can self modify")
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+draft.ID+"/apply", map[string]any{}, 200)
	after, err := s.aiProfile(ctx)
	if err != nil || after.Revision == before.Revision || *after.Effective.Instructions != instructions {
		t.Fatalf("approval: %+v %v", after, err)
	}
	if _, _, _, verified := s.activePlugin("0.3.0"); !verified {
		t.Fatal("external compatibility range not applied")
	}
	skills, err := s.writeDSHSkills()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(skills, "weir-security", "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "Updated host") {
		t.Fatalf("effective skills: %s %v", b, err)
	}
	n, err := s.Store.Ent.Resource.Query().Where(resource.KindEQ(aiProfileVersionKind)).Count(ctx)
	if err != nil || n != 2 {
		t.Fatalf("history: %d %v", n, err)
	}
	history := mustJSON(t, admin, "GET", "/api/v1/admin/ai/profile/history", nil, 200)
	for _, item := range history["items"].([]any) {
		row := item.(map[string]any)
		data, ok := row["data"].(map[string]any)
		if !ok || data["document"] == nil {
			t.Fatal("AI history omitted restorable document", row)
		}
		b, _ := json.Marshal(data["document"])
		var historic aiProfileDocument
		if json.Unmarshal(b, &historic) != nil || aiProfileRevision(historic) != row["key"] {
			t.Fatal("AI history cannot restore document")
		}
	}
	if err = s.saveAIProfile(ctx, before.Document, before.Revision, 1); err == nil {
		t.Fatal("stale revision accepted")
	}
	rollbackText, _ := json.Marshal(before.Document)
	d, err := s.submitActionDraft(ctx, "dsh", "restore previous approved profile", []DraftStep{{Action: "ai.profile.update", Args: map[string]any{"text": string(rollbackText)}}})
	if err != nil {
		t.Fatal(err)
	}
	mustJSON(t, admin, "POST", "/api/v1/admin/ai/drafts/"+d.ID+"/apply", map[string]any{}, 200)
	restored, _ := s.aiProfile(ctx)
	if restored.Revision != before.Revision {
		t.Fatal("rollback did not restore prior content")
	}
	activeSkills, err := s.writeDSHSkills()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(activeSkills, "weir-security", "SKILL.md"), []byte("unexpected modification"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.writeDSHSkills(); err == nil {
		t.Fatal("modified immutable skill accepted")
	}
}

func TestAIProfileConcurrentChangeInvalidatesDraft(t *testing.T) {
	s, admin := newInstalledServer(t)
	ctx := context.Background()
	v, _ := s.aiProfile(ctx)
	doc := v.Document
	instructions := "first"
	doc.Site.Instructions = &instructions
	text, _ := json.Marshal(doc)
	d, err := s.submitActionDraft(ctx, "dsh", "change instructions", []DraftStep{{Action: "ai.profile.update", Args: map[string]any{"text": string(text)}}})
	if err != nil {
		t.Fatal(err)
	}
	other := v.Document
	newInstructions := "newer"
	other.Site.Instructions = &newInstructions
	if err = s.saveAIProfile(ctx, other, v.Revision, 1); err != nil {
		t.Fatal(err)
	}
	if w := admin.do("POST", "/api/v1/admin/ai/drafts/"+d.ID+"/apply", nil, nil); w.Code != 409 {
		t.Fatalf("stale draft accepted: %d %s", w.Code, w.Body.String())
	}
}
