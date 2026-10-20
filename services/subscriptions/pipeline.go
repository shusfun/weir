// Operator semantics follow MiSub operator-runner.js (MIT); unsupported
// variants fail explicitly instead of executing JavaScript or losing fields.
package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/imzyb/MiGate/internal/subscription"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Operator struct {
	Type    string          `json:"type"`
	Enabled *bool           `json:"enabled,omitempty"`
	Params  json.RawMessage `json:"params"`
}

func (o *Operator) UnmarshalJSON(raw []byte) error {
	type wire Operator
	var value wire
	if err := decodeOperator(raw, &value); err != nil {
		return err
	}
	*o = Operator(value)
	return nil
}

func operatorProtocol(protocol string) string {
	if protocol == "shadowsocks" {
		return "ss"
	}
	return protocol
}

func nodeContext(n subscription.ParsedLink, index int, target string) map[string]any {
	region, zh, emoji := regionInfo(n.Remark, n.Address)
	metadata := ExtractNodeMetadata(n.OriginalName)
	if n.OriginalName == "" {
		metadata = ExtractNodeMetadata(n.Remark)
	}
	return map[string]any{"originalName": metadata.OriginalName, "cleanName": metadata.CleanName, "multiplier": metadata.Multiplier, "group": n.SourceGroup, "provider": n.SourceName, "name": n.Remark, "protocol": operatorProtocol(n.Protocol), "server": n.Address, "port": n.Port, "index": index + 1, "globalIndex": index + 1, "target": target, "region": region, "regionZh": zh, "emoji": emoji}
}
func rename(n *subscription.ParsedLink, name string) error {
	if len(name) > 1024 {
		return errors.New("node name too long")
	}
	if n.Protocol == "vmess" {
		payload := strings.TrimPrefix(n.RawLink, "vmess://")
		var b []byte
		var e error
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			b, e = encoding.DecodeString(payload)
			if e == nil {
				break
			}
		}
		if e != nil {
			return e
		}
		var v map[string]any
		if e = json.Unmarshal(b, &v); e != nil {
			return e
		}
		v["ps"] = name
		b, e = json.Marshal(v)
		if e != nil {
			return e
		}
		n.RawLink = "vmess://" + base64.StdEncoding.EncodeToString(b)
	} else {
		u, e := url.Parse(n.RawLink)
		if e != nil {
			return e
		}
		u.Fragment = name
		n.RawLink = u.String()
	}
	n.Remark = name
	return nil
}

// NodeMetadata contains display provenance only. Entries are aligned by occurrence,
// avoiding URL-map collisions when two sources contain the same endpoint.
type NodeMetadata struct {
	Manual   bool
	Group    string
	Provider string
}

func Run(links []string, operators []Operator, target string) ([]string, error) {
	return RunWithMetadata(links, operators, target, nil)
}
func RunWithMetadata(links []string, operators []Operator, target string, metadata []NodeMetadata) ([]string, error) {
	if metadata != nil && len(metadata) != len(links) {
		return nil, errors.New("node metadata count mismatch")
	}
	for _, m := range metadata {
		if len(m.Group) > 120 || len(m.Provider) > 200 || strings.ContainsAny(m.Group+m.Provider, "\x00\r\n") {
			return nil, errors.New("invalid node provenance")
		}
	}

	if len(links) > 10000 || len(operators) > 100 {
		return nil, errors.New("pipeline size limit")
	}
	parsed, e := subscription.ParseLinks(links)
	if e != nil {
		return nil, e
	}
	if len(parsed.Skipped) > 0 {
		return nil, errors.New("unrecognized nodes; refusing silent field loss")
	}
	nodes := parsed.Nodes
	for i := range nodes {
		nodes[i].OriginalName = nodes[i].Remark
		if metadata != nil {
			nodes[i].SourceGroup = metadata[i].Group
			nodes[i].SourceName = metadata[i].Provider
		}
	}

	for _, op := range operators {
		if op.Enabled != nil && !*op.Enabled {
			continue
		}
		switch op.Type {
		case "filter":
			var p struct {
				Include, Exclude struct {
					Enabled bool
					Rules   json.RawMessage
				}
				Protocols struct {
					Enabled bool
					Values  []string
				}
				Regions struct {
					Enabled bool
					Values  []string
				}
			}
			if e = decodeOperator(op.Params, &p); e != nil {
				return nil, e
			}
			include, exclude := []*regexp.Regexp{}, []*regexp.Regexp{}
			if p.Include.Enabled {
				include, e = filterRules(p.Include.Rules)
				if e != nil {
					return nil, e
				}
			}
			if p.Exclude.Enabled {
				exclude, e = filterRules(p.Exclude.Rules)
				if e != nil {
					return nil, e
				}
			}
			kept := nodes[:0]
			for _, n := range nodes {
				match := func(rules []*regexp.Regexp) (bool, error) {
					for _, re := range rules {
						region, zh, _ := regionInfo(n.Remark, n.Address)
						if re.MatchString(n.Remark) || re.MatchString(region) || re.MatchString(zh) {
							return true, nil
						}
					}
					return false, nil
				}
				if p.Include.Enabled && len(include) > 0 {
					ok, e := match(include)
					if e != nil {
						return nil, e
					}
					if !ok {
						continue
					}
				}
				if p.Exclude.Enabled {
					ok, e := match(exclude)
					if e != nil {
						return nil, e
					}
					if ok {
						continue
					}
				}
				if p.Protocols.Enabled {
					ok := false
					for _, v := range p.Protocols.Values {
						ok = ok || strings.EqualFold(v, operatorProtocol(n.Protocol))
					}
					if !ok {
						continue
					}
				}
				if p.Regions.Enabled {
					region, zh, _ := regionInfo(n.Remark, n.Address)
					ok := false
					for _, v := range p.Regions.Values {
						ok = ok || v == region || v == zh
					}
					if !ok {
						continue
					}
				}
				kept = append(kept, n)
			}
			nodes = kept
		case "dedup":
			var p struct{ IncludeProtocol *bool }
			if len(op.Params) > 0 {
				if e = decodeOperator(op.Params, &p); e != nil {
					return nil, e
				}
			}
			seen := map[string]int{}
			out := []subscription.ParsedLink{}
			for _, n := range nodes {
				key := n.Address + ":" + strconv.Itoa(n.Port)
				if p.IncludeProtocol == nil || *p.IncludeProtocol {
					key = n.Protocol + "|" + key
				}
				if idx, ok := seen[key]; ok {
					if len([]rune(n.Remark)) > len([]rune(out[idx].Remark)) {
						out[idx] = n
					}
				} else {
					seen[key] = len(out)
					out = append(out, n)
				}
			}
			nodes = out
		case "sort":
			if e = sortNodes(nodes, op.Params, target); e != nil {
				return nil, e
			}
		case "rename":
			var p struct {
				Regex struct {
					Enabled bool
					Rules   []json.RawMessage
				}
				Template struct {
					Enabled                     bool
					Template, IndexScope, Scope string
					Offset, IndexStart          int
				}
			}
			if e = decodeOperator(op.Params, &p); e != nil {
				return nil, e
			}
			contexts := make([]map[string]any, len(nodes))
			// Validate rule objects even when the input contains no nodes.
			for _, raw := range p.Regex.Rules {
				var text string
				if json.Unmarshal(raw, &text) != nil {
					var rule regexRule
					if err := decodeOperator(raw, &rule); err != nil {
						return nil, err
					}
				}
			}
			for i := range nodes {
				contexts[i] = nodeContext(nodes[i], i, target)
				if p.Regex.Enabled {
					name, err := regexRename(nodes[i].Remark, p.Regex.Rules, contexts[i])
					if err != nil {
						return nil, err
					}
					if err = rename(&nodes[i], name); err != nil {
						return nil, err
					}
					contexts[i]["name"] = name
				}
			}
			if p.Template.Enabled {
				scope := p.Template.IndexScope
				if scope == "" {
					scope = p.Template.Scope
				}
				if scope == "" {
					scope = "region"
				}
				if scope != "global" && scope != "protocol" && scope != "region" && scope != "regionProtocol" {
					return nil, errors.New("unsupported index scope")
				}
				counts := map[string]int{}
				for i := range nodes {
					ctx := contexts[i]
					key := "global"
					if scope == "protocol" {
						key = nodes[i].Protocol
					}
					if scope == "region" {
						key = str(ctx["regionZh"])
					}
					if scope == "regionProtocol" {
						key = str(ctx["regionZh"]) + "|" + nodes[i].Protocol
					}
					counts[key]++
					offset := p.Template.Offset
					if offset == 0 {
						offset = p.Template.IndexStart
					}
					if offset == 0 {
						offset = 1
					}
					ctx["index"] = counts[key] + offset - 1
					ctx["globalIndex"] = i + offset
					name, e := RenderOperatorTemplate(p.Template.Template, ctx)
					if e != nil {
						return nil, e
					}
					if e = rename(&nodes[i], name); e != nil {
						return nil, e
					}
				}
			}
		case "script":
			var p struct {
				DSL       []json.RawMessage `json:"dsl"`
				Code, URL string
			}
			if e = decodeOperator(op.Params, &p); e != nil {
				return nil, e
			}
			if p.Code != "" || p.URL != "" {
				return nil, errors.New("legacy JavaScript execution forbidden; use declarative DSL")
			}
			for _, raw := range p.DSL {
				var step struct{ Action, Template, Expression string }
				if e = json.Unmarshal(raw, &step); e != nil {
					return nil, e
				}
				switch step.Action {
				case "filter":
					if _, err := MatchCondition(map[string]any{}, raw); err != nil {
						return nil, err
					}
					out := []subscription.ParsedLink{}
					for i, n := range nodes {
						ok, e := MatchCondition(nodeContext(n, i, target), raw)
						if e != nil {
							return nil, e
						}
						if ok {
							out = append(out, n)
						}
					}
					nodes = out
				case "rename":
					if err := decodeOperator(raw, &step); err != nil {
						return nil, err
					}
					template := step.Template
					if template == "" {
						template = step.Expression
					}
					for i := range nodes {
						name, e := Render(template, nodeContext(nodes[i], i, target))
						if e != nil {
							return nil, e
						}
						if name != "" {
							if e = rename(&nodes[i], name); e != nil {
								return nil, e
							}
						}
					}
				default:
					return nil, errors.New("unsupported DSL action")
				}
			}
		default:
			return nil, fmt.Errorf("unsupported operator %s", op.Type)
		}
	}
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.RawLink
	}
	return out, nil
}

// PrependName preserves the original connection material; only its display name changes.
func PrependName(link, prefix string) (string, error) {
	if prefix == "" {
		return link, nil
	}
	if len(prefix) > 256 || strings.ContainsAny(prefix, "\x00\r\n") {
		return "", errors.New("invalid prefix")
	}
	n, e := subscription.ParseLink(link)
	if e != nil {
		return "", e
	}
	if strings.HasPrefix(n.Remark, prefix) {
		return link, nil
	}
	name := prefix
	if n.Remark != "" {
		name += " - " + n.Remark
	}
	if e = rename(&n, name); e != nil {
		return "", e
	}
	return n.RawLink, nil
}
