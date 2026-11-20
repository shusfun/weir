package model_test

import (
	"testing"

	"github.com/shusfun/weir/internal/kernelconf/model"
)

func TestInferInboundCoreAssignsProtocolOwner(t *testing.T) {
	for protocol, want := range map[string]string{
		"vless": model.CoreXray, "vmess": model.CoreXray, "trojan": model.CoreXray, "shadowsocks": model.CoreXray,
		"socks": model.CoreXray, "http": model.CoreXray, "unknown": model.CoreXray,
		"hysteria2": model.CoreSingbox, "tuic": model.CoreSingbox, "wireguard": model.CoreSingbox, "shadowtls": model.CoreSingbox,
		" TUIC ": model.CoreSingbox,
	} {
		if got := model.InferInboundCore(protocol); got != want {
			t.Fatalf("InferInboundCore(%q) = %s, want %s", protocol, got, want)
		}
	}
	if got := model.InboundCore(model.Inbound{Protocol: "vless", Core: "singbox"}); got != model.CoreSingbox {
		t.Fatalf("explicit core must win: %s", got)
	}
}
