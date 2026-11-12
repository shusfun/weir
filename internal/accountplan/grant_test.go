package accountplan

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestGrantStacking(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	future, past := now.AddDate(0, 0, 5), now.AddDate(0, 0, -5)
	for _, tc := range []struct {
		name    string
		expiry  *time.Time
		initial bool
		want    *time.Time
	}{
		{"active", &future, false, ptr(future.AddDate(0, 0, 10))},
		{"expired", &past, false, ptr(now.AddDate(0, 0, 10))},
		{"perpetual", nil, false, nil},
		{"initial", nil, true, ptr(now.AddDate(0, 0, 10))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Account{Quota: 100, Unlimited: true, ExpiresAt: tc.expiry, AllNodes: true, NodeIDs: []int{3, 1}}
			got, err := Apply(a, Grant{Quota: 50, Days: 10, NodeIDs: []int{2, 3}}, now, tc.initial)
			if err != nil || got.Quota != 150 || !got.Unlimited || !got.AllNodes || !reflect.DeepEqual(got.NodeIDs, []int{1, 2, 3}) || !reflect.DeepEqual(got.ExpiresAt, tc.want) {
				t.Fatalf("%+v %v", got, err)
			}
			if !reflect.DeepEqual(a.NodeIDs, []int{3, 1}) {
				t.Fatal("mutated source authorization")
			}
		})
	}
	if _, err := Apply(Account{Quota: math.MaxInt64}, Grant{Quota: 1}, now, false); err == nil {
		t.Fatal("quota overflow accepted")
	}
	for _, g := range []Grant{{Quota: -1}, {Days: 3651}, {NodeIDs: []int{1, 1}}, {ProfileIDs: []int{0}}} {
		if g.Validate() == nil {
			t.Fatal("invalid grant accepted", g)
		}
	}
}
func ptr(t time.Time) *time.Time { return &t }
