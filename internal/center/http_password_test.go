package center

import (
	"strings"
	"testing"
)

func TestValidAccountPassword(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "six digits", password: "123456", valid: true},
		{name: "six letters", password: "abcdef", valid: true},
		{name: "six unicode characters", password: "密码密码密码", valid: true},
		{name: "five characters", password: "12345", valid: false},
		{name: "over 72 bytes", password: strings.Repeat("a", 73), valid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validAccountPassword(tc.password); got != tc.valid {
				t.Fatalf("validAccountPassword(%q) = %t, want %t", tc.password, got, tc.valid)
			}
		})
	}
}
