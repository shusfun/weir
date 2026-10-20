package control

import (
	"encoding/json"
	"testing"
)

func TestAnnouncementValidation(t *testing.T) {
	a := defaultAnnouncement()
	raw, _ := json.Marshal(a)
	if _, e := decodeAnnouncement(raw); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`null`, `{}`, `{"type":"html"}`, `{"type":"info","content":"ok","unknown":1}`, `{"type":"info","enabled":true,"content":"ok"}`, `{"type":"info","enabled":true,"public":true}`, `{"type":"info","content":"\u0000"}`, `{"type":"info"} {}`} {
		if _, e := decodeAnnouncement([]byte(raw)); e == nil {
			t.Fatal("invalid announcement accepted", raw)
		}
	}
	valid := `{"enabled":true,"public":true,"authenticated":false,"title":"提醒","content":"<script>alert(1)</script>\nPlain text","type":"warning","dismissible":true}`
	parsed, e := decodeAnnouncement([]byte(valid))
	if e != nil || parsed.Content != "<script>alert(1)</script>\nPlain text" {
		t.Fatal("literal text changed", e)
	}
	if announcementRevision(a) == announcementRevision(parsed) {
		t.Fatal("revision did not change")
	}
}
