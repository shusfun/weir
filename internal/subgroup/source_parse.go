package subgroup

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/shusfun/weir/internal/sub"
	js "github.com/shusfun/weir/internal/sub/jscompat"
	"gopkg.in/yaml.v3"
)

var sourceProxiesKey = regexp.MustCompile(`(?m)^\s*(?:proxies|Proxy)\s*:`)

// A malformed structured document must never fall through to line parsing:
// otherwise a broken config or an HTML error page silently clears good nodes.
func parseSourceContent(body string, plusAsSpace bool) ([]string, error) {
	text := strings.TrimSpace(decodeContent(body))
	empty := text == ""
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		var doc map[string]json.RawMessage
		var outbounds []json.RawMessage
		if json.Unmarshal([]byte(text), &doc) != nil || doc["outbounds"] == nil || string(doc["outbounds"]) == "null" || json.Unmarshal(doc["outbounds"], &outbounds) != nil {
			return nil, errors.New("source_content_invalid")
		}
		empty = len(outbounds) == 0
	} else if sourceProxiesKey.MatchString(text) {
		var doc map[string]any
		decoder := yaml.NewDecoder(strings.NewReader(text))
		if decoder.Decode(&doc) != nil {
			return nil, errors.New("source_content_invalid")
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			return nil, errors.New("source_content_invalid")
		}
		proxies, exists := doc["proxies"]
		if !exists {
			proxies = doc["Proxy"]
		}
		list, ok := proxies.([]any)
		if !ok {
			return nil, errors.New("source_content_invalid")
		}
		empty = len(list) == 0
	}
	parsed := sub.ParseNodeList(text, sub.FixOptions{PlusAsSpace: plusAsSpace})
	if len(parsed) == 0 && !empty {
		return nil, errors.New("source_no_supported_nodes")
	}
	nodes := make([]string, 0, len(parsed))
	for _, p := range parsed {
		nodes = append(nodes, js.ToString(p.Get("url")))
	}
	return nodes, nil
}
