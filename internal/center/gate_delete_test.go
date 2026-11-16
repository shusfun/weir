package center

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shusfun/weir/internal/store"
	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/audit"
	"github.com/shusfun/weir/internal/store/ent/certificate"
	"github.com/shusfun/weir/internal/store/ent/credential"
	entexit "github.com/shusfun/weir/internal/store/ent/exit"
	"github.com/shusfun/weir/internal/store/ent/gate"
	"github.com/shusfun/weir/internal/store/ent/resource"
	"github.com/shusfun/weir/internal/store/ent/setting"
	"github.com/shusfun/weir/packages/contracts"
)

func TestGateDeletionPreviewAndAtomicCleanup(t *testing.T) {
	s, admin, fg := setupGate(t)
	ctx := t.Context()
	gid := int(fg.id)
	other := pairFakeGate(t, admin)
	direct := s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(gid), entexit.TagEQ("direct")).OnlyX(ctx)
	n := s.Store.Ent.Node.Create().SetGateID(gid).SetExitID(direct.ID).SetName("deleted-node").SetKernel("xray").SetProtocol("vless").SetPort(23456).SaveX(ctx)
	peerExit, err := s.persistExit(ctx, s.Store.Ent, int(other.id), nil, "", ExitSpec{Kind: "node", NodeID: n.ID, Remark: "dependent-exit"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	peer := s.Store.Ent.Node.Create().SetGateID(int(other.id)).SetExitID(peerExit.ID).SetName("retained-node").SetKernel("xray").SetProtocol("vless").SetPort(23457).SaveX(ctx)
	cert := s.Store.Ent.Certificate.Create().SetName("retained-acme").SetDomain("example.test").SetSource(certificate.SourceAcme).SetGateID(gid).SetCertPem("retained-cert").SetKeyPem("retained-sealed-key").SaveX(ctx)
	s.Store.Ent.Node.UpdateOneID(peer.ID).SetCertID(cert.ID).ExecX(ctx)
	laya := s.Store.Ent.Setting.Create().SetKey("laya").SetValue(map[string]any{"mode": "gate", "gate_id": gid, "enabled": true, "model": "retained-model"}).SaveX(ctx)
	u := s.Store.Ent.User.Create().SetEmail("delete-owner@example.test").SetPasswordHash("x").SetNodeIds([]int{n.ID, peer.ID}).SaveX(ctx)
	ch := s.Store.Ent.Chain.Create().SetUserID(u.ID).SetEntryNodeID(n.ID).SetExitID(direct.ID).SetName("deleted-chain").SaveX(ctx)
	l := s.Store.Ent.Link.Create().SetUserID(u.ID).SetToken("preserved-subscription-token").SetAllNodes(false).SetNodeIds([]int{n.ID, peer.ID}).SetChainIds([]int{ch.ID}).SaveX(ctx)
	s.Store.Ent.Credential.Create().SetUserID(u.ID).SetLinkID(l.ID).SetNodeID(n.ID).SetChainID(ch.ID).SetStatsKey("delete-me").SetUUID("old-uuid").SaveX(ctx)
	keep := s.Store.Ent.Credential.Create().SetUserID(u.ID).SetLinkID(l.ID).SetNodeID(peer.ID).SetStatsKey("keep-me").SetUUID("keep-uuid").SaveX(ctx)
	s.Store.Ent.Route.Create().SetGateID(gid).SetNodeID(n.ID).SetExitID(direct.ID).SetDomain("example.test").SaveX(ctx)
	base := "/api/v1/admin/gates/" + itoa(gid)
	mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 409)
	mustJSON(t, admin, "POST", base+"/revoke", map[string]any{}, 200)
	preview := mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
	p := preview["impact"].(map[string]any)
	if len(p["nodes"].([]any)) != 1 || len(p["blocked_exits"].([]any)) != 1 || len(p["links"].([]any)) != 1 {
		t.Fatal(p)
	}
	if len(p["certificates"].([]any)) != 1 || p["laya"] != true {
		t.Fatal("missing device dependencies", p)
	}
	if strings.Contains(jsonText(p), "retained-sealed-key") {
		t.Fatal("preview exposed certificate key")
	}
	confirm := map[string]any{"preview_token": preview["preview_token"]}
	// The same number of nodes with different configuration is still stale.
	s.Store.Ent.Node.UpdateOneID(n.ID).SetName("changed-after-preview").ExecX(ctx)
	mustJSON(t, admin, "DELETE", base, confirm, 409)
	preview = mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
	confirm["preview_token"] = preview["preview_token"]
	s.Store.Ent.Certificate.UpdateOneID(cert.ID).SetName("renamed-cert").ExecX(ctx)
	mustJSON(t, admin, "DELETE", base, confirm, 409)
	preview = mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
	confirm["preview_token"] = preview["preview_token"]
	s.Store.Ent.Setting.UpdateOneID(laya.ID).SetValue(map[string]any{"mode": "gate", "gate_id": gid, "enabled": true, "model": "updated-model"}).ExecX(ctx)
	mustJSON(t, admin, "DELETE", base, confirm, 409)
	preview = mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
	confirm["preview_token"] = preview["preview_token"]
	w := admin.do("DELETE", base, confirm, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if _, err := s.Store.Ent.Gate.Get(ctx, gid); !ent.IsNotFound(err) {
		t.Fatal("gate remained", err)
	}
	if _, err := s.Store.Ent.Node.Get(ctx, n.ID); !ent.IsNotFound(err) {
		t.Fatal("node remained", err)
	}
	if s.Store.Ent.Credential.Query().Where(credential.StatsKeyEQ("delete-me")).ExistX(ctx) {
		t.Fatal("credential remained")
	}
	l = s.Store.Ent.Link.GetX(ctx, l.ID)
	if l.Token != "preserved-subscription-token" || len(l.NodeIds) != 1 || l.NodeIds[0] != peer.ID || len(l.ChainIds) != 0 {
		t.Fatal("link was not preserved and cleaned")
	}
	if cr := s.Store.Ent.Credential.GetX(ctx, keep.ID); cr.UUID != "keep-uuid" {
		t.Fatal("unrelated credential rotated")
	}
	peerExit = s.Store.Ent.Exit.GetX(ctx, peerExit.ID)
	if peerExit.Kind != entexit.KindBlackhole || peerExit.ExitNodeID != nil || !peerExit.Enabled {
		t.Fatal("dependent path could leak direct", peerExit)
	}
	if len(s.Store.Ent.User.GetX(ctx, u.ID).NodeIds) != 1 {
		t.Fatal("stale authorization")
	}
	cert = s.Store.Ent.Certificate.GetX(ctx, cert.ID)
	if cert.GateID != nil || cert.CertPem != "retained-cert" || cert.KeyPem != "retained-sealed-key" || cert.Status != certificate.StatusFailed || cert.LastError == "" {
		t.Fatal("certificate material or detached status was lost")
	}
	if id := s.Store.Ent.Node.GetX(ctx, peer.ID).CertID; id == nil || *id != cert.ID {
		t.Fatal("shared certificate reference lost")
	}
	laya = s.Store.Ent.Setting.GetX(ctx, laya.ID)
	if laya.Value["enabled"] != false || laya.Value["gate_id"] != float64(0) || laya.Value["model"] != "updated-model" {
		t.Fatal("Laya did not detach or lost unrelated settings")
	}
	a := s.Store.Ent.Audit.Query().Where(audit.ActionEQ("gate.delete")).OnlyX(ctx)
	if a.Detail["trace_id"] != w.Header().Get("X-Trace-Id") {
		t.Fatal("missing correlated audit")
	}
	if code, body := fg.call("POST", "/api/v1/agent/poll", contracts.AgentPoll{Protocol: contracts.AgentProtocol}); code != 403 || !strings.Contains(string(body), "gate_revoked") {
		t.Fatalf("old identity: %d %s", code, body)
	}
	mustJSON(t, admin, "POST", "/api/v1/agent/challenge", map[string]any{"gate_id": gid}, 403)
	mustJSON(t, admin, "DELETE", base, confirm, 404)
}

type pausedGateMeterLock struct {
	store.Locker
	name             string
	paused           atomic.Bool
	entered, release chan struct{}
}

func (l *pausedGateMeterLock) Do(ctx context.Context, name string, fn func(context.Context) error) error {
	if name == l.name && l.paused.CompareAndSwap(false, true) {
		close(l.entered)
		select {
		case <-l.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return l.Locker.Do(ctx, name, fn)
}

func TestGateDeletionDiscardsWaitingMeterWrites(t *testing.T) {
	for _, kind := range []string{"settings", "sample"} {
		t.Run(kind, func(t *testing.T) {
			s, admin := newInstalledServer(t)
			fg := pairFakeGate(t, admin)
			gid := int(fg.id)
			ctx := t.Context()
			base := "/api/v1/admin/gates/" + itoa(gid)
			v := mustJSON(t, admin, "GET", base+"/traffic", nil, 200)
			p := mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
			l := &pausedGateMeterLock{Locker: s.Store.Lock, name: "gate-network:" + itoa(gid), entered: make(chan struct{}), release: make(chan struct{})}
			s.Store.Lock = l
			done := make(chan error, 1)
			go func() {
				if kind == "settings" {
					w := admin.do("PUT", base+"/traffic", map[string]any{"quota": 1000, "mode": "sum", "reset_day": 1, "revision": v["revision"]}, nil)
					if w.Code != 404 {
						done <- errors.New("waiting settings write must return gate_not_found: " + w.Body.String())
						return
					}
					done <- nil
					return
				}
				rx, tx := uint64(100), uint64(200)
				now := time.Now().UTC()
				done <- s.recordGateNetwork(ctx, gid, &contracts.HostSample{ObservedAt: now, BootID: "boot", NetRxBytes: &rx, NetTxBytes: &tx}, now)
			}()
			select {
			case <-l.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("meter write did not reach lock")
			}
			w := admin.do("DELETE", base, map[string]any{"preview_token": p["preview_token"]}, nil)
			close(l.release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			if s.Store.Ent.Resource.Query().Where(resource.KindIn(gateTrafficKind, gateTrafficConfigKind), resource.KeyEQ(itoa(gid))).ExistX(ctx) {
				t.Fatal("late write recreated a resource for the deleted Gate")
			}
		})
	}
}

func TestGateDeletionRollsBackOnAuditFailure(t *testing.T) {
	s, admin := newInstalledServer(t)
	fg := pairFakeGate(t, admin) // No heartbeat: deleting an unused pairing is allowed.
	ctx := t.Context()
	gid := int(fg.id)
	base := "/api/v1/admin/gates/" + itoa(gid)
	cert := s.Store.Ent.Certificate.Create().SetName("rollback-cert").SetDomain("example.test").SetSource(certificate.SourceAcme).SetGateID(gid).SetCertPem("cert").SetKeyPem("key").SaveX(ctx)
	s.Store.Ent.Setting.Create().SetKey("laya").SetValue(map[string]any{"mode": "gate", "gate_id": gid, "enabled": true}).SaveX(ctx)
	p := mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
	for _, key := range []string{"nodes", "chains", "exits", "blocked_exits", "routes", "users", "links", "credentials", "certificates"} {
		if _, ok := p["impact"].(map[string]any)[key].([]any); !ok {
			t.Fatalf("preview %s must be an array, including empty results", key)
		}
	}
	s.Store.Ent.Audit.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("audit unavailable")
		})
	})
	mustJSON(t, admin, "DELETE", base, map[string]any{"preview_token": p["preview_token"]}, 503)
	if !s.Store.Ent.Gate.Query().Where(gate.IDEQ(gid)).ExistX(ctx) || s.Store.Ent.Exit.Query().Where(entexit.GateIDEQ(gid)).CountX(ctx) != 1 {
		t.Fatal("partial delete escaped transaction")
	}
	if s.Store.Ent.Resource.Query().Where(resource.KindEQ(deletedGateKind)).ExistX(ctx) {
		t.Fatal("rollback left revocation tombstone")
	}
	if c := s.Store.Ent.Certificate.GetX(ctx, cert.ID); c.GateID == nil || *c.GateID != gid || c.Status != certificate.StatusReady {
		t.Fatal("certificate change escaped transaction")
	}
	if l := s.Store.Ent.Setting.Query().Where(setting.KeyEQ("laya")).OnlyX(ctx); l.Value["enabled"] != true {
		t.Fatal("Laya change escaped transaction")
	}
}

func TestGateDeletionRejectsForeignExpiredAndForgedPreview(t *testing.T) {
	s, admin, fg := setupGate(t)
	ctx := t.Context()
	gid := int(fg.id)
	base := "/api/v1/admin/gates/" + itoa(gid)
	s.Store.Ent.Gate.UpdateOneID(gid).SetStatus(gate.StatusRevoked).ExecX(ctx)
	p := mustJSON(t, admin, "POST", base+"/delete-preview", map[string]any{}, 200)
	raw, err := s.Crypto.Decrypt(p["preview_token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var claim gateDeleteClaim
	if err := json.Unmarshal([]byte(raw), &claim); err != nil {
		t.Fatal(err)
	}
	for _, altered := range []gateDeleteClaim{
		{GateID: gid, Actor: claim.Actor + 1, Revision: claim.Revision, Expires: claim.Expires},
		{GateID: gid + 1, Actor: claim.Actor, Revision: claim.Revision, Expires: claim.Expires},
		{GateID: gid, Actor: claim.Actor, Revision: claim.Revision, Expires: time.Now().Add(-time.Second)},
	} {
		data, e := json.Marshal(altered)
		if e != nil {
			t.Fatal(e)
		}
		token, e := s.Crypto.Encrypt(string(data))
		if e != nil {
			t.Fatal(e)
		}
		mustJSON(t, admin, "DELETE", base, map[string]any{"preview_token": token}, 409)
	}
	mustJSON(t, admin, "DELETE", base, map[string]any{"preview_token": "forged"}, 409)
	u := mustJSON(t, admin, "POST", "/api/v1/admin/users", map[string]any{"email": "delete-denied@example.test"}, 200)
	user := login(t, s, "delete-denied@example.test", u["password"].(string))
	mustJSON(t, user, "POST", base+"/delete-preview", map[string]any{}, 403)
	mustJSON(t, user, "DELETE", base, map[string]any{"preview_token": p["preview_token"]}, 403)
}
