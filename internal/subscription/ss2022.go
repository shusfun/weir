package subscription

import (
	"encoding/base64"
	"errors"
	"strings"
)

func validateSS2022(method, password string) error {
	if !strings.HasPrefix(method, "2022-") {
		return nil
	}
	size := 32
	switch method {
	case "2022-blake3-aes-128-gcm":
		size = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
	default:
		return errors.New("unsupported SS2022 method")
	}
	keys := strings.Split(password, ":")
	if len(keys) > 8 || method == "2022-blake3-chacha20-poly1305" && len(keys) != 1 {
		return errors.New("unsupported SS2022 identity chain")
	}
	for _, key := range keys {
		decoded, err := base64.StdEncoding.Strict().DecodeString(key)
		if err != nil || len(decoded) != size || base64.StdEncoding.EncodeToString(decoded) != key {
			return errors.New("invalid SS2022 key encoding or length")
		}
	}
	return nil
}
