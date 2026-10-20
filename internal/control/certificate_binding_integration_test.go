package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imzyb/MiGate/ent/resource"
	"github.com/imzyb/MiGate/ent/task"
)

func testCertificateBinding(t *testing.T, s *Server, request func(string, string, any, string, string) *httptest.ResponseRecorder, admin, alice string) {
	t.Helper()
	ctx := context.Background()
	d, err := s.Store.Device.Create().SetName("cert-bind-fixture").SetPublicKey(randomToken(32)).SetPairExpires(time.Now()).SetStatus("approved").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Device.DeleteOne(d).Exec(ctx)
	defer s.Store.Task.Delete().Where(task.DeviceIDEQ(d.ID)).Exec(ctx)
	defer s.Store.Resource.Delete().Where(resource.OwnerIDEQ(d.ID)).Exec(ctx)
	path := fmt.Sprintf("/api/v1/admin/devices/%d/tasks", d.ID)
	payload := map[string]any{"certificate_id": 7, "inbounds": []map[string]any{{"id": 1, "previous_certificate_id": 2}, {"id": 2, "previous_certificate_id": 2}}}
	body := map[string]any{"operation": "certificate.bind", "confirm": true, "payload": payload}
	if w := request("POST", path, body, "", alice); w.Code != 403 {
		t.Fatal("ordinary user binding", w.Code)
	}
	if w := request("POST", path, body, "", admin); w.Code != 400 {
		t.Fatal("old Agent binding", w.Code)
	}
	s.Store.Device.UpdateOne(d).SetMetadata(map[string]any{"capabilities": []string{"certificate.bind.v1"}}).SaveX(ctx)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(4), DNSNames: []string{"gate.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	s.Store.Resource.Create().SetKind("device-certificate").SetOwnerID(d.ID).SetKey(fmt.Sprintf("%d:7", d.ID)).SetData(map[string]any{"id": 7, "certificate_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}).SaveX(ctx)
	for _, id := range []int{1, 2} {
		s.Store.Resource.Create().SetKind("device-inbound").SetOwnerID(d.ID).SetKey(fmt.Sprintf("%d:%d", d.ID, id)).SetData(map[string]any{"id": id, "protocol": "trojan", "security": "tls", "tls_sni": "gate.invalid", "certificate_id": 2, "password": "private-fixture", "port": 24440 + id}).SaveX(ctx)
	}
	for _, p := range []map[string]any{{"certificate_id": 7, "inbounds": []map[string]any{{"id": 1, "previous_certificate_id": 99}}}, {"certificate_id": 7, "inbounds": []map[string]any{{"id": 99, "previous_certificate_id": 0}}}, {"certificate_id": 7, "inbounds": payload["inbounds"], "path": "/etc/x-ui"}} {
		w := request("POST", path, map[string]any{"operation": "certificate.bind", "confirm": true, "payload": p}, "", admin)
		if w.Code != 400 && w.Code != 409 {
			t.Fatal("unsafe binding accepted", w.Code)
		}
	}
	w := request("POST", path, body, "", admin)
	if w.Code != 202 {
		t.Fatal("binding enqueue failed", w.Code)
	}
	var queued struct{ ID int }
	json.Unmarshal(w.Body.Bytes(), &queued)
	r := s.Store.Resource.Query().Where(resource.KindEQ("device-inbound"), resource.KeyEQ(fmt.Sprintf("%d:1", d.ID))).OnlyX(ctx)
	if number(r.Data["certificate_id"]) != 2 {
		t.Fatal("enqueue changed applied state")
	}
	tx, _ := s.Store.Tx(ctx)
	if err := saveCertificateBinding(ctx, tx, d.ID, payload); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		r = s.Store.Resource.Query().Where(resource.KindEQ("device-inbound"), resource.KeyEQ(fmt.Sprintf("%d:%d", d.ID, id))).OnlyX(ctx)
		if number(r.Data["certificate_id"]) != 7 || r.Data["password"] != "private-fixture" || number(r.Data["port"]) != float64(24440+id) {
			t.Fatal("receipt lost unrelated attrs")
		}
	}
	t.Log("certificate batch admin/capability/resource/old-binding guards, queued state and transactional projection passed")
}
