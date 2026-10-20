package subscription

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

// InventoryIdentity is intentionally separate from the legacy outbound identity.
// Preserve every connection option; only display metadata may be discarded.
func InventoryIdentity(raw string) string {
	canonical := strings.TrimSpace(raw)
	if strings.HasPrefix(canonical, "vmess://") {
		if body, ok := tryDecodeBase64(strings.TrimPrefix(canonical, "vmess://")); ok {
			var values map[string]any
			if json.Unmarshal([]byte(body), &values) == nil {
				for _, key := range []string{"ps", "remark", "remarks", "name"} {
					delete(values, key)
				}
				if b, err := json.Marshal(values); err == nil {
					canonical = "vmess://" + string(b)
				}
			}
		}
	} else if u, err := url.Parse(canonical); err == nil {
		u.Fragment = ""
		u.RawFragment = ""
		q, err := url.ParseQuery(u.RawQuery)
		if err == nil {
			for _, key := range []string{"remarks", "remark", "name", "ps", "desc"} {
				q.Del(key)
			}
			u.RawQuery = q.Encode()
			canonical = u.String()
		}
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
