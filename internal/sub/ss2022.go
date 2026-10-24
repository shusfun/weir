package sub

import (
	"strings"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// 移植自 functions/modules/utils/ss2022-validator.js

// ss2022KeyLengths 对应 SS2022_KEY_LENGTHS（保持上游顺序）。
var ss2022KeyLengths = []struct {
	cipher string
	length int
}{
	{"2022-blake3-aes-128-gcm", 16},
	{"2022-blake3-aes-256-gcm", 32},
}

func ss2022ExpectedLength(cipher string) int {
	for _, e := range ss2022KeyLengths {
		if e.cipher == cipher {
			return e.length
		}
	}
	return 0
}

var reSIP002Body = js.MustRegExp(`^[A-Za-z0-9+/=-_]+$`, "")

// ValidateSS2022Node 对应 validateSS2022Node，返回 {valid, warning?, error?, details?}。
func ValidateSS2022Node(nodeURL any) *js.Object {
	u, ok := nodeURL.(string)
	if !ok || u == "" || !strings.HasPrefix(u, "ss://") {
		return js.Obj("valid", true)
	}
	atIndex := strings.Index(u, "@")
	hashIndex := strings.Index(u, "#")
	if atIndex == -1 {
		end := len(u)
		if hashIndex != -1 {
			end = hashIndex
		}
		possible := sliceRange(u, 5, end)
		if reSIP002Body.Test(possible) {
			return js.Obj("valid", true)
		}
		return js.Obj("valid", true, "warning", "SS 节点格式不标准 (缺少 @)")
	}
	base64Part := sliceRange(u, 5, atIndex)
	b, err := js.AtobBytes(base64Part)
	if err != nil {
		return js.Obj("valid", true, "warning", "无法解码 SS 节点: "+err.Error())
	}
	decoded := js.TextDecode(b)
	colonIndex := strings.Index(decoded, ":")
	if colonIndex == -1 {
		return js.Obj("valid", true, "warning", "SS 节点格式不标准 (缺少 :)")
	}
	cipher := decoded[:colonIndex]
	password := decoded[colonIndex+1:]
	if !strings.HasPrefix(cipher, "2022-") {
		return js.Obj("valid", true)
	}
	expected := ss2022ExpectedLength(cipher)
	if expected == 0 {
		return js.Obj("valid", true, "warning", "未知的 SS 2022 加密方式: "+cipher)
	}
	keyBytes, err := js.AtobBytes(password)
	if err != nil {
		return js.Obj("valid", false, "error", "SS 2022 密码不是有效的 Base64: "+err.Error(),
			"details", js.Obj("cipher", cipher, "password", js.Substring(password, 0, 20)+"..."))
	}
	if len(keyBytes) != expected {
		suggestion := "请联系节点提供商修正配置"
		var suggested any = js.Null
		for _, e := range ss2022KeyLengths {
			if e.length == len(keyBytes) {
				suggested = e.cipher
				suggestion = "建议使用 " + e.cipher + " 加密方式"
				break
			}
		}
		return js.Obj("valid", false, "error", "SS 2022 密钥长度不匹配", "details", js.Obj(
			"cipher", cipher, "expectedKeyLength", float64(expected), "actualKeyLength", float64(len(keyBytes)),
			"suggestedCipher", suggested, "suggestion", suggestion))
	}
	return js.Obj("valid", true)
}

// sliceRange 对应 str.substring(start, end)（参数为字节偏移，调用方保证来自 indexOf）。
func sliceRange(s string, start, end int) string {
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// FixSS2022Node 对应 fixSS2022Node。
func FixSS2022Node(nodeURL string) *js.Object {
	validation := ValidateSS2022Node(nodeURL)
	details := js.AsObject(validation.Get("details"))
	if js.Truthy(validation.Get("valid")) || !js.Truthy(details.Get("suggestedCipher")) {
		reason := "无法自动修复"
		if js.Truthy(validation.Get("valid")) {
			reason = "节点配置正确,无需修复"
		}
		return js.Obj("fixed", false, "originalUrl", nodeURL, "reason", reason)
	}
	atIndex := strings.Index(nodeURL, "@")
	base64Part := sliceRange(nodeURL, 5, atIndex)
	serverPart := nodeURL[atIndex:]
	b, err := js.AtobBytes(base64Part)
	if err != nil {
		return js.Obj("fixed", false, "originalUrl", nodeURL, "error", "修复失败: "+err.Error())
	}
	decoded := js.TextDecode(b)
	colonIndex := strings.Index(decoded, ":")
	password := decoded[colonIndex+1:]
	newConfig := js.ToString(details.Get("suggestedCipher")) + ":" + password
	fixedURL := "ss://" + js.Base64EncodeUTF8(newConfig) + serverPart
	return js.Obj("fixed", true, "originalUrl", nodeURL, "fixedUrl", fixedURL, "changes", js.Obj(
		"from", details.Get("cipher"), "to", details.Get("suggestedCipher"), "reason", details.Get("suggestion")))
}

// ValidateNodeList 对应 validateNodeList。
func ValidateNodeList(nodeURLs any) *js.Object {
	arr, ok := nodeURLs.(*js.Array)
	if !ok {
		return js.Obj("error", "输入必须是数组")
	}
	results := js.Obj("total", float64(arr.Len()), "valid", 0.0, "invalid", 0.0, "warnings", 0.0,
		"invalidNodes", js.Arr(), "warningNodes", js.Arr())
	inc := func(k string) { results.Set(k, results.Get(k).(float64)+1) }
	for i, u := range arr.Items {
		v := ValidateSS2022Node(u)
		if !js.Truthy(v.Get("valid")) {
			inc("invalid")
			results.Get("invalidNodes").(*js.Array).Push(js.Obj("index", float64(i), "url", u,
				"error", v.Get("error"), "details", v.Get("details")))
		} else if js.Truthy(v.Get("warning")) {
			inc("warnings")
			results.Get("warningNodes").(*js.Array).Push(js.Obj("index", float64(i), "url", u, "warning", v.Get("warning")))
		} else {
			inc("valid")
		}
	}
	return results
}
