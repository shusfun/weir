package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// PoolChange replaces exactly one registered pool in a single core transaction.
// Previous covers every member, including disabled members and transport secrets.
type PoolChange struct {
	PoolID   int64        `json:"pool_id"`
	Previous []PoolMember `json:"previous"`
	Next     []Outbound   `json:"next"`
}
type PoolMember struct {
	ID     int64  `json:"id"`
	Digest string `json:"digest"`
}

func PoolMemberDigest(out Outbound) string {
	// Normalize transport on both sides; core validation adds compatible defaults.
	s, err := ManagedOutboundSettings(out)
	if err != nil {
		return ""
	}
	b, _ := json.Marshal(s)
	out.SettingsJSON = string(b)
	out.Remark, out.LastSeenAt, out.RawLink = "", "", ""
	out.SupportedCores = nil
	out.Sort = 0
	b, _ = json.Marshal(out)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (change PoolChange) Validate(current []Outbound, routes []RoutingRule) error {
	if change.PoolID <= 0 || len(change.Previous) > 500 || len(change.Next) > 500 {
		return errors.New("invalid_pool_change")
	}
	previous := map[int64]string{}
	for _, p := range change.Previous {
		if p.ID <= 0 || len(p.Digest) != 64 || previous[p.ID] != "" {
			return errors.New("invalid_pool_snapshot")
		}
		previous[p.ID] = p.Digest
	}
	currentIDs := map[int64]Outbound{}
	for _, out := range current {
		currentIDs[out.ID] = out
		if out.Source == OutboundSourceSubscription && out.SubscriptionID == change.PoolID {
			if got := PoolMemberDigest(out); got == "" || previous[out.ID] != got {
				return errors.New("pool_members_changed")
			}
			delete(previous, out.ID)
		}
	}
	if len(previous) != 0 {
		return errors.New("pool_members_changed")
	}
	ids, tags, identities := map[int64]bool{}, map[string]bool{}, map[string]bool{}
	for _, out := range change.Next {
		if out.ID <= 0 || !out.Enabled || out.Tag == "" || len(out.Tag) > 100 || out.Source != OutboundSourceSubscription || out.SubscriptionID != change.PoolID || len(out.SubscriptionIdentity) != 64 || ids[out.ID] || tags[out.Tag] || identities[out.SubscriptionIdentity] {
			return errors.New("invalid_pool_member")
		}
		if err := ValidateOutboundProfile(out); err != nil {
			return errors.New("invalid_pool_profile")
		}
		if _, err := ManagedOutboundSettings(out); err != nil {
			return errors.New("invalid_pool_transport")
		}
		if old, found := currentIDs[out.ID]; found && (old.Source != OutboundSourceSubscription || old.SubscriptionID != change.PoolID || old.SubscriptionIdentity != out.SubscriptionIdentity) {
			return errors.New("pool_resource_collision")
		}
		for _, old := range current {
			if old.ID != out.ID && old.Tag == out.Tag {
				return errors.New("pool_tag_collision")
			}
		}
		ids[out.ID], tags[out.Tag], identities[out.SubscriptionIdentity] = true, true, true
	}
	for _, route := range routes {
		old, found := currentIDs[route.OutboundID]
		if found && old.Source == OutboundSourceSubscription && old.SubscriptionID == change.PoolID && !ids[old.ID] {
			// A disabled rule must not silently point to a different node on re-enable.
			return fmt.Errorf("pool_member_has_routes")
		}
	}
	return nil
}
