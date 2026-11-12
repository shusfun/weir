package gate

import (
	"strings"
	"testing"
)

func TestUserDeltaIgnoresUnchangedConfig(t *testing.T) {
	body := []byte(`{"inbounds":[{"tag":"in","protocol":"vless","settings":{"clients":[{"email":"a","id":"1"}]}}]}`)
	added, removed, only := userDelta(body, body)
	if !only || len(added) != 0 || len(removed) != 0 {
		t.Fatalf("unchanged config looked like a user edit: %+v %+v %v", added, removed, only)
	}
}

func TestUserDeltaDetectsOnlyUserChanges(t *testing.T) {
	oldBody := []byte(`{"inbounds":[{"tag":"in","protocol":"vless","settings":{"clients":[{"email":"a","id":"1"}]}}],"outbounds":[{"tag":"direct"}]}`)
	newBody := []byte(`{"inbounds":[{"tag":"in","protocol":"vless","settings":{"clients":[{"email":"b","id":"2"}]}}],"outbounds":[{"tag":"direct"}]}`)
	added, removed, only := userDelta(oldBody, newBody)
	if !only || len(added) != 1 || added[0].Email != "b" || len(removed) != 1 || removed[0].Email != "a" {
		t.Fatalf("user delta = %+v %+v %v", added, removed, only)
	}
	changed := []byte(`{"inbounds":[{"tag":"in","protocol":"vless","port":1,"settings":{"clients":[{"email":"a","id":"1"}]}}],"outbounds":[{"tag":"direct"}]}`)
	if _, _, only = userDelta(oldBody, changed); only {
		t.Fatal("port change was treated as a user-only update")
	}
}

func TestUserDeltaUpdatesSameEmail(t *testing.T) {
	oldBody := []byte(`{"inbounds":[{"tag":"in","protocol":"vless","settings":{"clients":[{"email":"a","id":"1","flow":"xtls-rprx-vision"}]}}]}`)
	newBody := []byte(`{"inbounds":[{"tag":"in","protocol":"vless","settings":{"clients":[{"email":"a","id":"2","flow":"xtls-rprx-vision"}]}}]}`)
	added, removed, only := userDelta(oldBody, newBody)
	if !only || len(removed) != 1 || removed[0].Email != "a" || len(added) != 1 || added[0].Email != "a" || added[0].Protocol != "vless" {
		t.Fatalf("same-email credential change was not an update: added=%+v removed=%+v only=%v", added, removed, only)
	}
}

func TestXrayRemoveArgsIncludeTagAndEmail(t *testing.T) {
	args := xrayRemoveArgs("127.0.0.1:8080", "in-1", "c-u1")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-tag=in-1") || !strings.HasSuffix(joined, "c-u1") || strings.Contains(joined, "stdin") {
		t.Fatalf("rmu args = %v", args)
	}
}
