package sub

import (
	"errors"
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 builtin-egern-generator.js 与 transformer-factory.js

// GenerateBuiltinEgernConfig 对应 generateBuiltinEgernConfig：以内置极简模板渲染 Egern 配置。
func GenerateBuiltinEgernConfig(nodeList string, options *js.Object) (string, error) {
	if options == nil {
		options = js.NewObject()
	}
	tpl := GetBuiltinTemplate("clash_misub_minimal")
	if tpl == nil {
		tpl = GetBuiltinTemplate("clash_acl4ssr_lite")
	}
	if tpl == nil || tpl.Content == "" {
		return "", errors.New("No builtin template available for Egern")
	}
	return RenderEgernFromIniTemplate(tpl.Content, js.Obj(
		"nodeList", nodeList,
		"fileName", js.Or(options.Get("fileName"), "MiSub"),
		"targetFormat", "egern",
		"ruleLevel", js.Or(options.Get("ruleLevel"), "std"),
		"interval", js.Or(options.Get("interval"), 86400.0),
		"managedConfigUrl", js.Or(options.Get("managedConfigUrl"), ""),
		"skipCertVerify", options.Get("skipCertVerify"),
		"enableUdp", options.Get("enableUdp"),
		"enableTfo", options.Get("enableTfo"),
		"addFlagEmoji", options.Get("addFlagEmoji"),
	))
}

// TransformBuiltinSubscription 对应 transformBuiltinSubscription：按目标格式选择内置生成器。
// 不支持的目标返回 ok=false（对应 null）。
func TransformBuiltinSubscription(nodeList string, targetFormat string, options *js.Object) (string, bool, error) {
	normalized := js.ToLower(targetFormat)
	key := normalized
	if strings.HasPrefix(normalized, "surge&ver=") {
		key = "surge"
	}
	switch key {
	case "clash":
		return GenerateBuiltinClashConfig(nodeList, options), true, nil
	case "surge":
		return GenerateBuiltinSurgeConfig(nodeList, options), true, nil
	case "loon":
		return GenerateBuiltinLoonConfig(nodeList, options), true, nil
	case "quanx":
		return GenerateBuiltinQuanxConfig(nodeList, options), true, nil
	case "singbox", "sing-box":
		return GenerateBuiltinSingboxConfig(nodeList, options), true, nil
	case "egern":
		out, err := GenerateBuiltinEgernConfig(nodeList, options)
		return out, true, err
	}
	return "", false, nil
}
