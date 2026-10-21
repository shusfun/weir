package control

import (
	"strings"
	"testing"
)

func TestValidAccountPassword(t *testing.T) {
	if !validAccountPassword("Ab1cde") {
		t.Fatal("six-character mixed password rejected")
	}
	for _, password := range []string{"abcdef", "Abcdef", "ab1cde", "AB1CDE", "Ab1", "Ab1" + strings.Repeat("x", 70)} {
		if validAccountPassword(password) {
			t.Fatalf("accepted %q", password)
		}
	}
}
