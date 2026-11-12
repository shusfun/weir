package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
)

// GateObservation is replaceable runtime state, kept outside business rows.
// SeenAt can outlive the online window to preserve the last observation while
// the offline checkpoint is pending. Digest only acknowledges a saved snapshot.
type GateObservation struct {
	SeenAt time.Time      `json:"seen_at"`
	Digest string         `json:"digest,omitempty"`
	Host   map[string]any `json:"host,omitempty"`
}

func (s *Store) GateObservation(ctx context.Context, id int) (*GateObservation, error) {
	raw, ok, err := s.Transient.Get(ctx, "gate-observation:"+strconv.Itoa(id))
	if err != nil || !ok {
		return nil, err
	}
	var out GateObservation
	if json.Unmarshal([]byte(raw), &out) != nil || out.SeenAt.IsZero() || out.SeenAt.After(time.Now().Add(time.Minute)) {
		return nil, errors.New("invalid gate observation")
	}
	return &out, nil
}

func (s *Store) SetGateObservation(ctx context.Context, id int, out GateObservation) error {
	if id < 1 || out.SeenAt.IsZero() {
		return errors.New("invalid gate observation")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return s.Transient.Set(ctx, "gate-observation:"+strconv.Itoa(id), string(raw), 24*time.Hour)
}

// Every entity read presents the latest runtime observation to online checks,
// including eager loads and transaction reads. SQL predicates on last_seen_at
// remain checkpoint predicates and must not be used as the online filter.
func (s *Store) observeGateReads() {
	s.Ent.Gate.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			value, err := next.Query(ctx, q)
			if err != nil {
				return value, err
			}
			var gates []*ent.Gate
			switch v := value.(type) {
			case *ent.Gate:
				gates = []*ent.Gate{v}
			case []*ent.Gate:
				gates = v
			default:
				return value, nil
			}
			for _, g := range gates {
				if g == nil {
					continue
				}
				observed, e := s.GateObservation(ctx, g.ID)
				if e != nil {
					return nil, e
				}
				if observed == nil {
					continue
				}
				if g.LastSeenAt == nil || observed.SeenAt.After(*g.LastSeenAt) {
					at := observed.SeenAt
					g.LastSeenAt = &at
				}
				if observed.Host != nil {
					g.Host = observed.Host
				}
			}
			return value, nil
		})
	}))
}
