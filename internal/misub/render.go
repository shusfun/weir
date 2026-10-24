package misub

import (
	"encoding/base64"
	"errors"
	"net/url"
	"path"
	"strings"

	"github.com/shusfun/weir/internal/sub"
	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// Template 是订阅组使用的规则模板：内置（6 个）、自定义（存在中心）或远程地址（中心拉取并缓存）。
type Template struct {
	Kind string // none / builtin / custom / remote
	Text string // 模板内容（内置时由 ID 查得）
	INI  bool
}

// BuiltinTemplate 按 ID 返回内置模板。
func BuiltinTemplate(id string) (Template, bool) {
	t := sub.GetBuiltinTemplate(id)
	if t == nil {
		return Template{}, false
	}
	return Template{Kind: "builtin", Text: t.Content, INI: t.Format == "ini"}, true
}

// IsINIPath 判断远程模板地址是否为 ini 模板。
func IsINIPath(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	p := raw
	if err == nil {
		p = u.Path
	}
	return strings.EqualFold(path.Ext(p), ".ini")
}

// RenderOptions 对应上游 builtinOptions。
type RenderOptions struct {
	FileName       string
	RuleLevel      string // none / lite / std / full（模板为远程或自定义时为 none）
	Interval       int
	SkipCertVerify bool
	EnableUDP      bool
	EnableTFO      bool
	UserAgent      string
	Query          string // 原始查询串（客户端识别、dns-mode 等）
	ManagedURL     string
	DNSMode        string
	CustomDNS      string
}

// Formats 是支持的订阅目标格式。
var Formats = []string{"base64", "clash", "singbox", "surge", "loon", "quanx", "egern"}

// Target 按查询参数与 UA 判断目标格式（对应上游 determineTargetFormat）。
func Target(userAgent, rawQuery string) string {
	return sub.DetermineTargetFormat(userAgent, js.ParseSearchParams(rawQuery))
}

// IsBrowser 判断是否浏览器访问。
func IsBrowser(userAgent string) bool { return sub.IsBrowserAgent(userAgent) }

func (o RenderOptions) object() *js.Object {
	sp := js.ParseSearchParams(o.Query)
	dns := o.DNSMode
	if dns == "" {
		dns = "clean"
	}
	interval := o.Interval
	if interval == 0 {
		interval = 86400
	}
	level := o.RuleLevel
	if level == "" {
		level = "std"
	}
	return js.Obj(
		"userAgent", o.UserAgent, "searchParams", sp, "hiddifyCompatible", sub.IsHiddifyAgent(o.UserAgent),
		"dnsMode", dns, "fileName", o.FileName, "managedConfigUrl", o.ManagedURL, "interval", float64(interval),
		"skipCertVerify", o.SkipCertVerify, "enableUdp", o.EnableUDP, "enableTfo", o.EnableTFO, "ruleLevel", level,
		"regionOverrides", js.Arr(), "isMeta", sub.IsMetaCore(o.UserAgent, sp), "customDnsOverride", o.CustomDNS,
	)
}

// Render 输出订阅内容：base64 直接编码；其他格式用内置生成器，有 ini 模板时按模板渲染策略组与规则，
// 有 Clash YAML 完整配置模板时只替换其中的节点。返回内容与 Content-Type。
func Render(nodeList, target string, o RenderOptions, t Template) ([]byte, string, error) {
	if target == "base64" || target == "" || target == "v2ray" || target == "trojan" || target == "nodes" {
		return []byte(base64.StdEncoding.EncodeToString([]byte(nodeList))), "text/plain; charset=utf-8", nil
	}
	opts := o.object()
	content, ok, err := sub.TransformBuiltinSubscription(nodeList, target, opts)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return []byte(base64.StdEncoding.EncodeToString([]byte(nodeList))), "text/plain; charset=utf-8", nil
	}
	contentType := "text/plain; charset=utf-8"
	if t.Text != "" && !js.Truthy(opts.Get("hiddifyCompatible")) {
		params := js.Obj("nodeList", nodeList, "fileName", o.FileName, "targetFormat", target, "ruleLevel", opts.Get("ruleLevel"),
			"interval", opts.Get("interval"), "managedConfigUrl", o.ManagedURL, "skipCertVerify", o.SkipCertVerify,
			"enableUdp", o.EnableUDP, "isMeta", opts.Get("isMeta"), "customDnsOverride", o.CustomDNS, "dnsMode", opts.Get("dnsMode"))
		switch {
		case t.INI:
			switch target {
			case "clash":
				if content, err = sub.RenderClashFromIniTemplate(t.Text, params); err != nil {
					return nil, "", err
				}
			case "singbox", "sing-box":
				content = sub.RenderSingboxFromIniTemplate(t.Text, params)
			case "surge", "surge&ver=4":
				content = sub.RenderSurgeFromIniTemplate(t.Text, params)
			case "loon":
				content = sub.RenderLoonFromIniTemplate(t.Text, params)
			case "quanx":
				content = sub.RenderQuanxFromIniTemplate(t.Text, params)
			case "egern":
				if content, err = sub.RenderEgernFromIniTemplate(t.Text, params); err != nil {
					return nil, "", err
				}
			}
		case target == "clash":
			if c, ok := clashYAMLProfile(t.Text, nodeList, opts); ok {
				content = c
			}
		}
	}
	switch target {
	case "clash", "egern":
		contentType = "application/x-yaml; charset=utf-8"
	case "singbox", "sing-box":
		contentType = "application/json; charset=utf-8"
	}
	if strings.TrimSpace(content) == "" {
		return nil, "", errors.New("empty_output")
	}
	return []byte(content), contentType, nil
}

// clashYAMLProfile 对应上游 renderClashYamlProfileTemplate：保留模板的策略组与规则，替换节点并收紧本地监听。
func clashYAMLProfile(text, nodeList string, opts *js.Object) (string, bool) {
	doc, err := js.YAMLLoad(text)
	cfg, isObj := doc.(*js.Object)
	if err != nil || !isObj {
		return "", false
	}
	if _, ok := cfg.Get("proxy-groups").(*js.Array); !ok {
		return "", false
	}
	if _, ok := cfg.Get("rules").(*js.Array); !ok {
		return "", false
	}
	var urls []string
	for _, l := range strings.Split(nodeList, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			urls = append(urls, l)
		}
	}
	proxies := sub.URLsToClashProxies(urls, opts)
	for _, p := range proxies {
		for _, k := range p.Keys() {
			if strings.HasPrefix(k, "_") {
				p.Delete(k)
			}
		}
	}
	sub.DeduplicateNames(proxies)
	list := js.Arr()
	for _, p := range proxies {
		list.Push(p)
	}
	cfg.Set("allow-lan", false)
	cfg.Set("bind-address", "127.0.0.1")
	cfg.Set("ipv6", false)
	cfg.Set("external-controller", "127.0.0.1:9090")
	cfg.Set("dns", sub.ResolveSafeDNSConfig(cfg.Get("dns"), sub.SafeDNSOptions{Mode: opts.Get("dnsMode"), ProxyGroup: "🌐 DNS 出口"}))
	cfg.Set("proxies", list)
	out, err := js.YAMLDump(cfg, js.YAMLDumpOptions{DoubleQuote: true})
	if err != nil {
		return "", false
	}
	return out, true
}

// TemplateInfo 是内置模板的说明（列表展示用）。
type TemplateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Format      string `json:"format"`
}

// BuiltinTemplates 列出 6 个内置规则模板。
func BuiltinTemplates() []TemplateInfo {
	ids := []string{"clash_misub_minimal", "clash_acl4ssr_lite", "clash_misub_media_ai", "clash_acl4ssr_full", "clash_exclusive_ai", "clash_game_optimized"}
	out := make([]TemplateInfo, 0, len(ids))
	for _, id := range ids {
		if t := sub.GetBuiltinTemplate(id); t != nil {
			out = append(out, TemplateInfo{ID: t.ID, Name: t.Name, Description: t.Description, Format: t.Format})
		}
	}
	return out
}

// CheckTemplate 检查自定义模板：ini 至少能解析出策略组或规则；yaml 必须是含 proxy-groups 与 rules 的 Clash 配置。
func CheckTemplate(format, content string) error {
	if format == "yaml" {
		doc, err := js.YAMLLoad(content)
		cfg, ok := doc.(*js.Object)
		if err != nil || !ok {
			return errors.New("不是有效的 YAML 对象")
		}
		if _, ok := cfg.Get("proxy-groups").(*js.Array); !ok {
			return errors.New("缺少 proxy-groups")
		}
		if _, ok := cfg.Get("rules").(*js.Array); !ok {
			return errors.New("缺少 rules")
		}
		return nil
	}
	model := sub.ParseIniTemplate(content, js.Obj())
	groups, _ := model.Get("groups").(*js.Array)
	rules, _ := model.Get("rules").(*js.Array)
	if (groups == nil || groups.Len() == 0) && (rules == nil || rules.Len() == 0) {
		return errors.New("没有解析出任何策略组或规则")
	}
	return nil
}

// ParseContent 解析粘贴的内容（节点链接、Base64、Clash YAML、sing-box JSON），返回节点链接。
func ParseContent(text string) []string {
	parsed := sub.ParseNodeList(decodeContent(text), sub.FixOptions{})
	out := make([]string, 0, len(parsed))
	for _, p := range parsed {
		if u := js.ToString(p.Get("url")); u != "" {
			out = append(out, u)
		}
	}
	return out
}
