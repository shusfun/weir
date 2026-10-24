package sub

import (
	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/subscription/template-pipeline.js：ini 模板 → 统一模型 → 各客户端格式。

// DeduplicateNames 对应 deduplicateNames：就地把重名节点改为 name_1、name_2。
func DeduplicateNames(proxies []*js.Object) {
	namer := NewUniqueNamer()
	for _, p := range proxies {
		if p != nil && js.Truthy(p.Get("name")) {
			p.Set("name", namer.GetUniqueNameValue(p.Get("name")))
		}
	}
}

// buildIniModel 是各 renderXFromIniTemplate 的公共部分。
func buildIniModel(templateText any, options *js.Object) *js.Object {
	if options == nil {
		options = js.NewObject()
	}
	var proxies []*js.Object
	if a, ok := options.Get("proxies").(*js.Array); ok {
		for _, p := range a.Items {
			proxies = append(proxies, objOrEmpty(p))
		}
		DeduplicateNames(proxies)
	} else {
		proxies = URLsToClashProxies(nodeListURLs(options), options)
		DeduplicateNames(proxies)
	}
	opts := options.Clone()
	opts.Set("proxies", objs(proxies))
	if a, ok := options.Get("proxies").(*js.Array); ok {
		// 上游直接复用调用方传入的数组
		opts.Set("proxies", a)
	}
	return ApplySmartModelOptimizations(ParseIniTemplate(templateText, opts))
}

// objs 把对象切片转换为 JS 数组。
func objs(list []*js.Object) *js.Array {
	a := js.Arr()
	for _, o := range list {
		a.Push(o)
	}
	return a
}

// RenderClashFromIniTemplate 对应 renderClashFromIniTemplate。
func RenderClashFromIniTemplate(templateText any, options *js.Object) (string, error) {
	return RenderClashFromTemplateModel(buildIniModel(templateText, options))
}

// RenderSingboxFromIniTemplate 对应 renderSingboxFromIniTemplate。
func RenderSingboxFromIniTemplate(templateText any, options *js.Object) string {
	return RenderSingboxFromTemplateModel(buildIniModel(templateText, options), options)
}

// RenderSurgeFromIniTemplate 对应 renderSurgeFromIniTemplate。
func RenderSurgeFromIniTemplate(templateText any, options *js.Object) string {
	return RenderSurgeFromTemplateModel(buildIniModel(templateText, options), options)
}

// RenderLoonFromIniTemplate 对应 renderLoonFromIniTemplate。
func RenderLoonFromIniTemplate(templateText any, options *js.Object) string {
	return RenderLoonFromTemplateModel(buildIniModel(templateText, options), options)
}

// RenderQuanxFromIniTemplate 对应 renderQuanxFromIniTemplate。
func RenderQuanxFromIniTemplate(templateText any, options *js.Object) string {
	return RenderQuanxFromTemplateModel(buildIniModel(templateText, options), options)
}

// RenderEgernFromIniTemplate 对应 renderEgernFromIniTemplate。
func RenderEgernFromIniTemplate(templateText any, options *js.Object) (string, error) {
	return RenderEgernFromTemplateModel(buildIniModel(templateText, options))
}
