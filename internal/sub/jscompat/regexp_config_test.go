package jscompat

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigReplacementBounded(t *testing.T) {
	re, err := NewConfigRegExp("(?<letter>a)", "g")
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"$1", "$<letter>", "$&", "$$", "$`", "$'", "[$1]-$<letter>"} {
		want := re.Replace("a b a", replacement)
		got, err := re.ReplaceBounded("a b a", replacement, 100)
		if err != nil || got != want {
			t.Fatalf("replacement %q: got %q want %q err=%v", replacement, got, want, err)
		}
	}
	if _, err := re.ReplaceBounded(strings.Repeat("a", 100), strings.Repeat("$&", 50), 100); !errors.Is(err, ErrRegExpOutputLimit) {
		t.Fatalf("expanded output limit not enforced: %v", err)
	}
	if _, err := re.ReplaceBounded("a", strings.Repeat("x", 101), 100); !errors.Is(err, ErrRegExpOutputLimit) {
		t.Fatalf("replacement input limit not enforced: %v", err)
	}
	plain, _ := NewConfigRegExp("z", "")
	if got, err := plain.ReplaceBounded("abc", "x", 3); err != nil || got != "abc" {
		t.Fatalf("valid unchanged result rejected: %q %v", got, err)
	}
}
