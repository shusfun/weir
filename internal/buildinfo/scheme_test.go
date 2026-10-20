package buildinfo

import "testing"

func TestParseSchemeBounds(t *testing.T) {
	valid := []string{"0.2.0", "v0.2.0", "9.99.999", "MiGate version: v1.2.3"}
	for _, raw := range valid {
		if _, ok := ParseScheme(raw); !ok {
			t.Fatalf("expected %q to parse", raw)
		}
	}
	invalid := []string{"", "0.2.0-rc114", "10.0.0", "9.100.0", "9.99.1000", "00.2.0", "0.02.0", "0.2.00", "1.2", "1.2.3.4"}
	for _, raw := range invalid {
		if _, ok := ParseScheme(raw); ok {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestSchemeNextIncrementsPatchUntilCeiling(t *testing.T) {
	current, _ := ParseScheme("0.2.0")
	next, ok := current.Next()
	if !ok || next.String() != "0.2.1" {
		t.Fatalf("next = %s ok=%v", next, ok)
	}
	rolled, _ := ParseScheme("0.2.999")
	next, ok = rolled.Next()
	if !ok || next.String() != "0.3.0" {
		t.Fatalf("patch carry = %s ok=%v", next, ok)
	}
	minor, _ := ParseScheme("0.99.999")
	next, ok = minor.Next()
	if !ok || next.String() != "1.0.0" {
		t.Fatalf("minor carry = %s ok=%v", next, ok)
	}
	ceiling, _ := ParseScheme("9.99.999")
	if _, ok = ceiling.Next(); ok {
		t.Fatal("9.99.999 must not grow a new version")
	}
}

func TestSchemeCompareOrdersMajorMinorPatch(t *testing.T) {
	older, _ := ParseScheme("0.2.0")
	newer, _ := ParseScheme("0.2.1")
	if older.Compare(newer) >= 0 || newer.Compare(older) <= 0 || older.Compare(older) != 0 {
		t.Fatal("compare order mismatch")
	}
}
