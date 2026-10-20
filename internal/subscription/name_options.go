package subscription

import "strings"

// DecodeInventoryWithNameOptions follows MiSub 4952c16 node-cleaner.js's
// opt-in plusAsSpace fragment policy. Only literal '+' in a URI name changes:
// credentials, queries, VMess JSON names and encoded %2B remain untouched.
// Unlike upstream, repeated percent decoding is deliberately not performed.
func DecodeInventoryWithNameOptions(body []byte, plusAsSpace bool) ([]string, error) {
	return decodeInventory(body, plusAsSpace)
}

func normalizeURINamePlus(link string) string {
	if strings.HasPrefix(link, "vmess://") {
		return link
	}
	base, name, found := strings.Cut(link, "#")
	if found && strings.Contains(name, "+") {
		return base + "#" + strings.ReplaceAll(name, "+", "%20")
	}
	return link
}
