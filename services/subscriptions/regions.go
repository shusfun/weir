package subscriptions

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// MiSub 4952c16 geo-utils.js (MIT). Preserve upstream keyword ordering and
// first-occurrence ASCII word-boundary semantics for parity.
//
//go:embed regions.json
var regionData []byte

type regionEntry struct {
	Name     string
	Keywords []string
	Emoji    string
}

var regions = func() []regionEntry {
	var result []regionEntry
	if err := json.Unmarshal(regionData, &result); err != nil {
		panic(err)
	}
	return result
}()

func asciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func asciiLetters(s string) bool {
	for i := range s {
		if !asciiLetter(s[i]) {
			return false
		}
	}
	return s != ""
}
func extractRegion(name string) string {
	name = strings.ToLower(name)
	for _, r := range regions {
		for _, keyword := range r.Keywords {
			k := strings.ToLower(keyword)
			idx := strings.Index(name, k)
			if idx < 0 {
				continue
			}
			if len(k) <= 3 && asciiLetters(k) {
				if idx > 0 && asciiLetter(name[idx-1]) {
					continue
				}
				end := idx + len(k)
				if end < len(name) && asciiLetter(name[end]) {
					continue
				}
			}
			return r.Name
		}
	}
	return "其他"
}
func regionInfo(name, server string) (code, zh, emoji string) {
	zh = extractRegion(name)
	if zh == "其他" {
		zh = extractRegion(server)
	}
	for _, r := range regions {
		if r.Name == zh {
			code = zh
			for _, k := range r.Keywords {
				if len(k) >= 2 && len(k) <= 3 && asciiLetters(k) {
					code = strings.ToUpper(k)
					break
				}
			}
			return code, zh, r.Emoji
		}
	}
	return zh, zh, "🌍"
}

// RegionLabel uses the pinned MiSub name/server keyword mapping. It is a
// presentation hint, never a geographic measurement or authorization signal.
func RegionLabel(name, server string) string {
	_, label, _ := regionInfo(name, server)
	return label
}
