package subscriptions

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/imzyb/MiGate/internal/subscription"
)

func TestNaturalSortFlagsAndPriority(t *testing.T) {
	names := []string{"🇯🇵 Node 10", "🇭🇰 node 2", "Node 1"}
	links := []string{}
	for i, name := range names {
		links = append(links, "ss://YWVzLTEyOC1nY206cGFzcw@example.com:"+strconv.Itoa(8000+i)+"#"+url.PathEscape(name))
	}
	out, err := Run(links, []Operator{{Type: "sort", Params: json.RawMessage(`{"keys":[{"key":"name"}]}`)}}, "base64")
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != links[2] || out[1] != links[1] || out[2] != links[0] {
		t.Fatal("natural/flag sort differs")
	}
}

func TestUpstreamSortComparison(t *testing.T) {
	if os.Getenv("MIGATE_UPSTREAM_TESTS") != "1" {
		t.Skip("local Node upstream comparison")
	}
	names := []string{"Node 10", "node 2", "Node 01", "🇭🇰 HK 2", "🇯🇵 JP 10", "🇯🇵 JP 2", "USA", "SG", "台湾", "Écho 2", "echo 10", "Echo 1", "unknown", "节点 10", "节点 2"}
	servers := []string{"10.0.0.10", "10.0.0.2", "2.0.0.1", "2001:db8::1", "node10.example", "node2.example"}
	nodes := []subscription.ParsedLink{}
	for i, name := range names {
		nodes = append(nodes, subscription.ParsedLink{RawLink: strconv.Itoa(i), Remark: name, Protocol: []string{"vless", "trojan", "vmess", "hysteria2", "shadowsocks", "tuic"}[i%6], Address: servers[i%len(servers)], Port: 8000 + i%4})
	}
	samples := []map[string]any{}
	for _, key := range []string{"name", "server", "port", "region", "regionZh", "protocol", "group"} {
		for _, order := range []string{"asc", "desc"} {
			for _, ignore := range []bool{true, false} {
				for _, custom := range [][]string{nil, {"日本", "香港", "tuic", "ss"}} {
					params := sortParams{Keys: []sortKey{{Key: key, Order: order, CustomOrder: custom}, {Key: "name", Order: "asc"}}, NameIgnoreEmoji: &ignore}
					raw, _ := json.Marshal(params)
					sorted := append([]subscription.ParsedLink{}, nodes...)
					if err := sortNodes(sorted, raw, "base64"); err != nil {
						t.Fatal(err)
					}
					records := []map[string]any{}
					expected := []string{}
					for i, n := range nodes {
						r := nodeContext(n, i, "base64")
						r["_id"] = n.RawLink
						records = append(records, r)
					}
					for _, n := range sorted {
						expected = append(expected, n.RawLink)
					}
					// Upstream JSON uses camelCase for custom orders and options.
					jsKeys := []map[string]any{}
					for _, k := range params.Keys {
						jsKeys = append(jsKeys, map[string]any{"key": k.Key, "order": k.Order, "customOrder": k.CustomOrder})
					}
					samples = append(samples, map[string]any{"params": map[string]any{"keys": jsKeys, "nameIgnoreEmoji": ignore}, "records": records, "expected": expected})
				}
			}
		}
	}
	data, _ := json.Marshal(samples)
	path := filepath.Join(t.TempDir(), "sort.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("node", filepath.Join("..", "..", "third_party", "misub", "reference", "compare-sort.mjs"), path).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", output, err)
	}
	t.Logf("%d sort configurations compared against fixed MiSub comparator", len(samples))
}
