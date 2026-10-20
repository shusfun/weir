package subscription

import (
	"strconv"
	"strings"
)

// UsageMetadata is upstream-reported subscription information, never the local
// user's enforced quota. Missing counters stay unknown, including on failure.
type UsageMetadata struct {
	Status   string  `json:"status"`
	Upload   *string `json:"upload,omitempty"`
	Download *string `json:"download,omitempty"`
	Total    *string `json:"total,omitempty"`
	Expire   *string `json:"expire,omitempty"`
}

type FetchedInventory struct {
	Body  []byte
	Usage UsageMetadata
}

func ParseUsageMetadata(headers []string) UsageMetadata {
	result := UsageMetadata{Status: "absent"}
	if len(headers) == 0 {
		return result
	}
	invalid := UsageMetadata{Status: "invalid"}
	if len(headers) != 1 || len(headers[0]) > 4096 || strings.TrimSpace(headers[0]) == "" {
		return invalid
	}
	seen := map[string]bool{}
	for _, item := range strings.Split(headers[0], ";") {
		if strings.TrimSpace(item) == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		var target **string
		switch key {
		case "upload":
			target = &result.Upload
		case "download":
			target = &result.Download
		case "total":
			target = &result.Total
		case "expire":
			target = &result.Expire
		default:
			// Unknown upstream extensions are never stored or exposed.
			continue
		}
		value = strings.TrimSpace(value)
		if !ok || seen[key] || value == "" {
			return invalid
		}
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				return invalid
			}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return invalid
		}
		seen[key] = true
		// Decimal strings retain exact counters through JSON, PG and JavaScript.
		canonical := strconv.FormatInt(n, 10)
		*target = &canonical
	}
	if len(seen) > 0 {
		result.Status = "available"
	}
	return result
}
