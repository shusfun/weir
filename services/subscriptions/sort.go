// Adapted from MiSub node-transformer.js, commit 4952c16 (MIT).
package subscriptions

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/imzyb/MiGate/internal/subscription"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

type sortKey struct {
	Key, Order  string
	CustomOrder []string
}
type sortParams struct {
	Keys            []sortKey
	NameIgnoreEmoji *bool
}

var ipv4SortPattern = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

func stripSortFlag(s string) string {
	r := []rune(s)
	if len(r) >= 2 && r[0] >= 0x1f1e6 && r[0] <= 0x1f1ff && r[1] >= 0x1f1e6 && r[1] <= 0x1f1ff {
		s = string(r[2:])
	}
	return strings.TrimSpace(s)
}

func sortNodes(nodes []subscription.ParsedLink, raw json.RawMessage, target string) error {
	var p sortParams
	if err := decodeOperator(raw, &p); err != nil {
		return err
	}
	if len(p.Keys) > 32 {
		return errors.New("sort key limit")
	}
	orders := make([]map[string]int, len(p.Keys))
	for i, k := range p.Keys {
		switch k.Key {
		case "", "name", "server", "port", "protocol", "region", "regionZh", "group":
		default:
			return errors.New("unsupported sort key")
		}
		values := k.CustomOrder
		if len(values) > 1000 {
			return errors.New("sort custom order limit")
		}
		if len(values) == 0 {
			if k.Key == "region" {
				values = []string{"香港", "台湾", "日本", "新加坡", "美国", "韩国", "英国", "德国", "法国", "加拿大"}
			}
			if k.Key == "protocol" {
				values = []string{"vless", "trojan", "vmess", "hysteria2", "ss", "ssr", "anytls"}
			}
		}
		orders[i] = map[string]int{}
		for j, v := range values {
			orders[i][v] = j
		}
	}
	// Pin en-US rather than inherit the developer's OS locale. MiSub's
	// localeCompare(undefined) otherwise changes order between environments.
	c := collate.New(language.English, collate.Numeric, collate.IgnoreCase, collate.IgnoreDiacritics)
	contexts := make(map[subscription.ParsedLink]map[string]any, len(nodes))
	for i, n := range nodes {
		contexts[n] = nodeContext(n, i, target)
	}
	compareIP := func(a, b string) int {
		x, y := ipv4SortPattern.FindStringSubmatch(a), ipv4SortPattern.FindStringSubmatch(b)
		if x != nil && y != nil {
			for i := 1; i <= 4; i++ {
				av, _ := strconv.Atoi(x[i])
				bv, _ := strconv.Atoi(y[i])
				if av < bv {
					return -1
				}
				if av > bv {
					return 1
				}
			}
			return 0
		}
		if x != nil {
			return -1
		}
		if y != nil {
			return 1
		}
		return c.CompareString(a, b)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		for ki, k := range p.Keys {
			if k.Key == "" {
				continue
			}
			a, b := str(contexts[nodes[i]][k.Key]), str(contexts[nodes[j]][k.Key])
			if k.Key == "region" {
				a = str(contexts[nodes[i]]["regionZh"])
				b = str(contexts[nodes[j]]["regionZh"])
			}
			cmp := 0
			switch k.Key {
			case "name":
				if p.NameIgnoreEmoji == nil || *p.NameIgnoreEmoji {
					a = stripSortFlag(a)
					b = stripSortFlag(b)
				}
				cmp = c.CompareString(a, b)
			case "port":
				if nodes[i].Port < nodes[j].Port {
					cmp = -1
				} else if nodes[i].Port > nodes[j].Port {
					cmp = 1
				}
			case "server":
				cmp = compareIP(a, b)
			default:
				ai, aok := orders[ki][a]
				bi, bok := orders[ki][b]
				if !aok {
					ai = 1 << 30
				}
				if !bok {
					bi = 1 << 30
				}
				if ai < bi {
					cmp = -1
				} else if ai > bi {
					cmp = 1
				} else {
					cmp = c.CompareString(a, b)
				}
			}
			if cmp != 0 {
				if strings.EqualFold(k.Order, "desc") {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	})
	return nil
}
