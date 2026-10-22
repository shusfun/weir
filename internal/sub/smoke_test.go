package sub

import (
	"strings"
	"testing"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 不依赖 Node 的冒烟测试：没有 node 时对照测试会跳过，这里保证基本链路可用。
func TestBuiltinOutputsSmoke(t *testing.T) {
	nodeList := strings.Join(sampleLinks, "\n")
	out, ok, err := TransformBuiltinSubscription(nodeList, "clash", js.Obj("ruleLevel", "full"))
	if err != nil || !ok {
		t.Fatalf("生成 Clash 失败: %v", err)
	}
	doc, err := js.YAMLLoad(out)
	if err != nil {
		t.Fatalf("生成的 Clash YAML 无法解析: %v", err)
	}
	proxies := objGet(doc, "proxies").(*js.Array)
	if proxies.Len() < 30 {
		t.Fatalf("节点数量异常: %d", proxies.Len())
	}
	for _, target := range []string{"surge", "loon", "quanx", "singbox", "egern"} {
		out, ok, err := TransformBuiltinSubscription(nodeList, target, nil)
		if err != nil || !ok || len(out) < 500 {
			t.Errorf("生成 %s 失败: ok=%v err=%v len=%d", target, ok, err, len(out))
		}
	}
	if _, err := js.JSONParse(GenerateBuiltinSingboxConfig(nodeList, nil)); err != nil {
		t.Errorf("sing-box 输出不是合法 JSON: %v", err)
	}
	for _, id := range BuiltinTemplateIDs {
		if _, err := RenderClashFromIniTemplate(GetBuiltinTemplate(id).Content, js.Obj("nodeList", nodeList)); err != nil {
			t.Errorf("模板 %s 渲染失败: %v", id, err)
		}
	}
}

func TestParseRoundTripSmoke(t *testing.T) {
	nodes := ParseNodeList(strings.Join(sampleLinks, "\n"), FixOptions{})
	if len(nodes) < 40 {
		t.Fatalf("解析节点数量异常: %d", len(nodes))
	}
	for _, p := range URLsToClashProxies(sampleLinks, nil) {
		if _, ok := ConvertClashProxyToURL(p); !ok && !js.StrictEquals(p.Get("type"), "socks5-tls") {
			t.Errorf("无法转换回链接: %s", js.Describe(p))
		}
	}
}
