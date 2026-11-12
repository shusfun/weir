package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskPayloadRejectsSemanticBoundsBeforeDispatch(t *testing.T) {
	for _, item := range []struct{ kind, body string }{
		{ActionCoreRestart, `{"kernel":"../../bin/sh"}`},
		{ActionLiveStart, `{"seconds":301}`},
		{ActionRealityProbe, `{"targets":[]}`},
		{ActionRealityProbe, `{"targets":["example.org/path"]}`},
		{ActionPanelConfigure, `{"nonce":"one-time","port":0,"https":false}`},
		{ActionPanelCert, `{"cert_id":-1,"ip":"1.1.1.1"}`},
		{ActionACMEHTTP01, `{"token":"token","key_auth":"other.thumbprint","seconds":150,"host":"example.org"}`},
		{ActionAssetFetch, `{"upload_id":"../credential","urls":["https://github.com/a/b"],"sha256":"` + strings.Repeat("a", 64) + `","size":1,"max_size":1}`},
		{TaskSubscriptionFetch, `{"credentials":{"url":"https://user:secret@example.org"},"max_bytes":100}`},
		{TaskExternalConvert, `{"credentials":{"url":"https://example.org"},"content":"","content_type":"text/plain\r\nX-Secret: bad","max_bytes":100}`},
		{TaskTraceRead, `{"trace_id":"` + strings.Repeat("0", 32) + `","limit":10}`},
		{TaskLayaClassify, `{"model":"laya:multilingual","state":"text","questions":{"decision":{"type":"shell"}}}`},
	} {
		if err := ValidateTaskPayload(item.kind, 1, json.RawMessage(item.body)); err == nil {
			t.Fatalf("accepted invalid %s", item.kind)
		}
	}
	for _, item := range []struct{ kind, body string }{
		{ActionLiveStart, `{"seconds":60}`},
		{ActionPanelConfigure, `{"nonce":"one-time","port":18276,"https":false}`},
		{ActionACMEHTTP01, `{"token":"token","key_auth":"token.thumbprint","seconds":150,"host":"1.1.1.1"}`},
		{TaskSubscriptionFetch, `{"credentials":{"url":"https://example.org/sub?token=secret"},"max_bytes":1024}`},
		{TaskLayaClassify, `{"model":"laya:multilingual","state":"text","questions":{"decision":{"type":"choice","criteria":{"yes":"analysis needed","no":"no analysis"}}}}`},
	} {
		if err := ValidateTaskPayload(item.kind, 1, json.RawMessage(item.body)); err != nil {
			t.Fatalf("rejected valid %s: %v", item.kind, err)
		}
	}
}
