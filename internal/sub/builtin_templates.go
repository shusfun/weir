package sub

import (
	"embed"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/builtin-template-registry.js：6 个内置 ini 分流模板。
// 模板正文原样保存在 templates/ 目录（由上游注册表逐字导出），通过 embed 打包进二进制。

//go:embed templates/*.ini
var builtinTemplateFS embed.FS

// BuiltinTemplate 是内置模板元数据与正文。
type BuiltinTemplate struct {
	ID, Name, Target, Format, Description, Content string
}

// Value 转换为与上游一致的 JS 对象。
func (t *BuiltinTemplate) Value() *js.Object {
	return js.Obj("id", t.ID, "name", t.Name, "target", t.Target, "format", t.Format, "description", t.Description, "content", t.Content)
}

// BuiltinTemplateIDs 保持上游注册表顺序。
var BuiltinTemplateIDs = []string{
	"clash_misub_minimal", "clash_acl4ssr_lite", "clash_misub_media_ai", "clash_acl4ssr_full", "clash_exclusive_ai", "clash_game_optimized",
}

var builtinTemplateMeta = map[string][2]string{
	"clash_misub_minimal":  {"MiSub 极简默认分流", "更偏日常使用的极简模板，仅保留主选择、自动选择、常用媒体与兜底分组，适合作为统一模板主线默认配置。"},
	"clash_acl4ssr_lite":   {"ACL4SSR 精简分流", "内置精简 ACL4SSR 分流模板，保留常用分组和核心规则，适合作为轻量默认预设。"},
	"clash_misub_media_ai": {"MiSub 流媒体与 AI 分流", "面向常见流媒体和 AI 服务场景的内置模板，保留自动选择、地区组和核心服务分流。"},
	"clash_acl4ssr_full":   {"ACL4SSR 完整分流", "内置完整 ACL4SSR 分流模板，适合作为 Clash 系列的高完整度规则预设。"},
	"clash_exclusive_ai":   {"MiSub 深度 AI 开发者模板", "专为 AI 开发者优化，强化 OpenAI/Claude 路由，增加固定节点漂移保护与纯净度检测引导。"},
	"clash_game_optimized": {"MiSub 游戏加速竞技模板", "强化各平台游戏规则（Steam, Epic, Sony 等），优先选择低延迟线路。"},
}

// GetBuiltinTemplate 对应 getBuiltinTemplate；不存在返回 nil。
func GetBuiltinTemplate(id string) *BuiltinTemplate {
	meta, ok := builtinTemplateMeta[id]
	if !ok {
		return nil
	}
	content, err := builtinTemplateFS.ReadFile("templates/" + id + ".ini")
	if err != nil {
		return nil
	}
	return &BuiltinTemplate{ID: id, Name: meta[0], Target: "clash", Format: "ini", Description: meta[1], Content: string(content)}
}

// BuiltinTemplateRegistryValue 对应 BUILTIN_TEMPLATE_REGISTRY（JS 对象形式）。
func BuiltinTemplateRegistryValue() *js.Object {
	o := js.NewObject()
	for _, id := range BuiltinTemplateIDs {
		o.Set(id, GetBuiltinTemplate(id).Value())
	}
	return o
}
