package subscription

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestSS2022KeyLengthEncodingAndIdentityChain(t *testing.T) {
	for _, method := range []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305"} {
		size := 32
		if strings.Contains(method, "128") {
			size = 16
		}
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", size)))
		keys := []string{key}
		if !strings.Contains(method, "chacha") {
			keys = append(keys, key+":"+key)
		}
		for _, secret := range keys {
			for _, user := range []string{url.UserPassword(method, secret).String(), base64.RawStdEncoding.EncodeToString([]byte(method + ":" + secret))} {
				n, err := ParseLink("ss://" + user + "@example.com:8388#SS2022")
				if err != nil || n.Password != secret {
					t.Fatal("valid SS2022 credentials lost", err)
				}
			}
		}
		for _, secret := range []string{"not-base64", key + "\n", strings.TrimRight(key, "="), base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", size-1))), key + ":"} {
			if _, err := ParseLink("ss://" + url.UserPassword(method, secret).String() + "@example.com:8388"); err == nil {
				t.Fatal("invalid SS2022 credential accepted")
			}
		}
		if strings.Contains(method, "chacha") && validateSS2022(method, key+":"+key) == nil {
			t.Fatal("unsupported ChaCha identity chain accepted")
		}
	}
	if validateSS2022("2022-unknown", "any") == nil {
		t.Fatal("unknown SS2022 method accepted")
	}
}
