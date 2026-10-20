package agent

import (
	"github.com/imzyb/MiGate/packages/contracts"
	"testing"
)

func TestOutboundTransportTaskRejectsIgnoredFieldsAndKeepsPrevious(t *testing.T) {
	a := &Agent{}
	good := contracts.Task{Operation: "outbound.upsert", Revision: 1, Payload: map[string]any{"id": 1, "tag": "vmess-test", "protocol": "vmess", "address": "example.com", "port": 443, "username": "12345678-1234-1234-1234-123456789abc", "settings_json": `{"network":"ws","security":"tls","path":"/ws","sni":"example.com"}`}}
	if err := a.mutate(good); err != nil {
		t.Fatal(err)
	}
	before := a.State.Outbounds[0].SettingsJSON
	bad := good
	bad.Revision = 2
	bad.Payload = map[string]any{}
	for k, v := range good.Payload {
		bad.Payload[k] = v
	}
	bad.Payload["settings_json"] = `{"network":"ws","security":"tls","unknown_option":true}`
	if err := a.mutate(bad); err == nil {
		t.Fatal("ignored transport accepted")
	}
	if len(a.State.Outbounds) != 1 || a.State.Outbounds[0].SettingsJSON != before {
		t.Fatal("failed transport changed current outbound")
	}
	if err := a.mutate(good); err != nil || len(a.State.Outbounds) != 1 {
		t.Fatal("replay added outbound", err)
	}
}
