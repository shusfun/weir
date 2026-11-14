package subout

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/shusfun/weir/internal/subscription"
)

// Formats 是支持的订阅目标格式。
var Formats = []string{"base64", "clash", "singbox", "surge", "loon", "quanx", "egern"}

// NodeExclusion describes a node omitted from one client target without exposing its link or credentials.
type NodeExclusion struct {
	Ref        string `json:"ref,omitempty"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol,omitempty"`
	ReasonCode string `json:"reason_code"`
	Reason     string `json:"reason"`
}

type GroupImpact struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
	Reason  string `json:"reason"`
}

type RuleImpact struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

// CompatibilityReport is safe to expose to the subscription owner; it never contains connection data.
type CompatibilityReport struct {
	Target         string          `json:"target"`
	IncludedNodes  int             `json:"included_nodes"`
	ExcludedNodes  []NodeExclusion `json:"excluded_nodes"`
	AffectedGroups []GroupImpact   `json:"affected_groups"`
	AffectedRules  []RuleImpact    `json:"affected_rules"`
	Warnings       []RuleImpact    `json:"warnings,omitempty"`
}

func emptyReport(target string) CompatibilityReport {
	if target == "" {
		target = "base64"
	}
	return CompatibilityReport{
		Target:         target,
		ExcludedNodes:  []NodeExclusion{},
		AffectedGroups: []GroupImpact{},
		AffectedRules:  []RuleImpact{},
	}
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Render keeps the original API while omitting nodes that cannot be represented by the requested target.
func Render(links []string, target string) ([]byte, string, error) {
	body, contentType, _, err := RenderDetailed(links, target)
	return body, contentType, err
}

// RenderDetailed renders compatible links and reports nodes omitted for this target.
func RenderDetailed(links []string, target string) ([]byte, string, CompatibilityReport, error) {
	report := emptyReport(target)
	if !knownTarget(target) {
		return nil, "", report, fmt.Errorf("unsupported target %q", target)
	}
	kept := make([]string, 0, len(links))
	for i, link := range links {
		if err := validateTargetLink(link, target); err != nil {
			report.ExcludedNodes = append(report.ExcludedNodes, exclusionFor(link, fmt.Sprintf("node:%d", i+1), err))
			continue
		}
		kept = append(kept, link)
	}
	report.IncludedNodes = len(kept)
	if len(kept) == 0 {
		return nil, "", report, fmt.Errorf("no compatible nodes for %s", report.Target)
	}
	body, contentType, err := renderRaw(kept, target)
	return body, contentType, report, err
}

func validateTargetLink(link, target string) error {
	if strings.TrimSpace(link) == "" || strings.ContainsAny(link, "\r\n") {
		return fmt.Errorf("invalid node link")
	}
	// Generic subscriptions preserve protocols understood by the receiving client.
	if target == "" || target == "base64" {
		return nil
	}
	parsed, err := subscription.ParseLinks([]string{link})
	if err != nil || len(parsed.Nodes) != 1 || len(parsed.Skipped) != 0 {
		return fmt.Errorf("invalid node link")
	}
	_, _, err = renderRaw([]string{link}, target)
	return err
}

func knownTarget(target string) bool {
	switch target {
	case "", "base64", "clash", "mihomo", "singbox", "sing-box", "surge", "loon", "quanx", "egern":
		return true
	default:
		return false
	}
}

func renderRaw(links []string, target string) ([]byte, string, error) {
	switch target {
	case "base64", "":
		return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))), "text/plain; charset=utf-8", nil
	case "clash", "mihomo":
		b, err := clashOutput(links)
		return b, "application/yaml; charset=utf-8", err
	case "singbox", "sing-box":
		b, err := singboxOutput(links)
		return b, "application/json", err
	case "surge", "loon", "quanx":
		b, err := iniOutput(links, target)
		return b, "text/plain; charset=utf-8", err
	case "egern":
		b, err := egernOutput(links)
		return b, "application/yaml; charset=utf-8", err
	}
	return nil, "", fmt.Errorf("unsupported target %q", target)
}

func exclusionFor(link, ref string, err error) NodeExclusion {
	name, protocol := "节点", ""
	if parsed, parseErr := subscription.ParseLinks([]string{link}); parseErr == nil && len(parsed.Nodes) > 0 {
		name, protocol = parsed.Nodes[0].Remark, parsed.Nodes[0].Protocol
	}
	if name == "" {
		name = "节点"
	}
	code, reason := exclusionReason(err)
	return NodeExclusion{Ref: ref, Name: name, Protocol: protocol, ReasonCode: code, Reason: reason}
}

func exclusionReason(err error) (string, string) {
	message := err.Error()
	switch {
	case strings.Contains(message, "does not support"), strings.Contains(message, "protocol unsupported"), strings.Contains(message, "unsupported Surge protocol"), strings.Contains(message, "unsupported Loon protocol"), strings.Contains(message, "unsupported Egern protocol"):
		return "unsupported_protocol", "此客户端不支持该节点协议"
	case strings.Contains(message, "unsupported"), strings.Contains(message, "cannot be represented"), strings.Contains(message, "cannot preserve"), strings.Contains(message, "not adapted"), strings.Contains(message, "requires TLS"):
		return "unsupported_parameters", "此节点包含目标客户端无法无损表达的参数"
	default:
		return "invalid_or_unsupported", "节点链接无效或包含无法安全转换的设置"
	}
}

// TargetForUA 按客户端 User-Agent 推断格式；无法识别时为 base64（通用节点链接列表）。
func TargetForUA(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox") || strings.Contains(ua, "hiddify") || strings.Contains(ua, "sfa") || strings.Contains(ua, "sfi") || strings.Contains(ua, "sfm"):
		return "singbox"
	case strings.Contains(ua, "clash") || strings.Contains(ua, "mihomo") || strings.Contains(ua, "stash") || strings.Contains(ua, "flclash") || strings.Contains(ua, "nyanpasu") || strings.Contains(ua, "verge"):
		return "clash"
	case strings.Contains(ua, "surge"):
		return "surge"
	case strings.Contains(ua, "loon"):
		return "loon"
	case strings.Contains(ua, "quantumult"):
		return "quanx"
	case strings.Contains(ua, "egern"):
		return "egern"
	}
	return "base64"
}
