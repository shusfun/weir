package contracts

import "testing"

func TestObservedDomainRejectsURLsAndAddresses(t *testing.T) {
	for _, v := range []string{"https://a.example/path", "1.2.3.4", "[::1]", "a.example:443", "user@a.example", "*.example", "a..example", " a.example", "-a.example", "a.example/path", "a.example?token=private"} {
		if ObservedDomain(v) != "" {
			t.Fatalf("accepted %q", v)
		}
	}
	if ObservedDomain("Domestic.Example.") != "domestic.example" {
		t.Fatal("canonical identity changed")
	}
}
