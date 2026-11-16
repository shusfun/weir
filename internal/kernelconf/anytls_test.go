package kernelconf

import (
	"encoding/json"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestAnyTLSInboundPreservesPerCredentialIdentity(t *testing.T) {
	cfg := contracts.GateConfig{
		Ports:       contracts.PortPlan{SingboxStats: 21002},
		Drivers:     map[string]string{"sing-box": driverText(t, "sing-box-1.12")},
		Inbounds:    []contracts.Inbound{{ID: 1, Tag: "anytls", Kernel: "sing-box", Protocol: "anytls", Port: 24443, Network: "tcp", Security: "tls", CertID: 5}},
		Credentials: []contracts.Credential{cred(1, 1, "", "one"), cred(2, 1, "", "two")},
	}
	out, err := Build(cfg, Options{CertDir: certs(t)})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Inbounds []struct {
			Type  string `json:"type"`
			Users []struct {
				Name     string `json:"name"`
				Password string `json:"password"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if err = json.Unmarshal(out.Singbox, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Inbounds) != 1 || doc.Inbounds[0].Type != "anytls" || len(doc.Inbounds[0].Users) != 2 {
		t.Fatalf("missing anytls users: %s", out.Singbox)
	}
	users := doc.Inbounds[0].Users
	if users[0].Name == users[1].Name || users[0].Password != "one" || users[1].Password != "two" {
		t.Fatal("AnyTLS merged credential identity")
	}
}
