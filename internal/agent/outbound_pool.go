package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/imzyb/MiGate/internal/db"
	"github.com/imzyb/MiGate/packages/contracts"
)

func (a *Agent) applyOutboundPool(t contracts.Task) error {
	var change db.PoolChange
	b, err := json.Marshal(t.Payload)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&change) != nil {
		return errors.New("invalid_pool_change")
	}
	key := fmt.Sprintf("pool:%d", change.PoolID)
	if a.State.Versions[key] == t.Revision && t.Revision > 0 {
		// State and receipts are durable separately. A crash between their writes
		// must replay the already applied state, rather than report a false failure.
		return nil
	}
	if a.State.Versions[key] > t.Revision {
		return errors.New("stale_pool_revision")
	}
	if err = change.Validate(a.State.Outbounds, a.State.Routing); err != nil {
		return err
	}
	kept := make([]db.Outbound, 0, len(a.State.Outbounds)+len(change.Next))
	for _, out := range a.State.Outbounds {
		if out.Source != db.OutboundSourceSubscription || out.SubscriptionID != change.PoolID {
			kept = append(kept, out)
		}
	}
	for _, out := range change.Next {
		s, _ := db.ManagedOutboundSettings(out)
		raw, _ := json.Marshal(s)
		out.SettingsJSON = string(raw)
		kept = append(kept, out)
	}
	a.State.Outbounds = kept
	if a.State.Versions == nil {
		a.State.Versions = map[string]int64{}
	}
	a.State.Versions[key] = t.Revision
	return nil
}
