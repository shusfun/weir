package center

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

type workerResourceObservation struct {
	Resources  contracts.WorkerResources `json:"resources"`
	ObservedAt time.Time                 `json:"observed_at"`
}

func (s *Server) recordWorkerResources(ctx context.Context, id int, res contracts.WorkerResources) error {
	if res.Validate() != nil || id < 1 {
		return errors.New("invalid worker resource observation")
	}
	if res.SourcesObservedAt != nil && res.SourcesObservedAt.After(time.Now().Add(time.Minute)) {
		return errors.New("invalid source observation timestamp")
	}
	for _, observation := range res.Components {
		if observation.ObservedAt.After(time.Now().Add(time.Minute)) {
			return errors.New("invalid worker component timestamp")
		}
	}
	b, _ := json.Marshal(workerResourceObservation{res, time.Now().UTC()})
	return s.Store.Transient.Set(ctx, "worker-resources:"+itoa(id), string(b), 90*time.Second)
}
func (s *Server) workerResources(ctx context.Context, id int) (*workerResourceObservation, error) {
	raw, ok, err := s.Store.Transient.Get(ctx, "worker-resources:"+itoa(id))
	if err != nil || !ok {
		return nil, err
	}
	var out workerResourceObservation
	if err = contracts.DecodeStrict([]byte(raw), &out, 4096); err != nil {
		return nil, err
	}
	if out.Resources.Validate() != nil || !freshObservation(out.ObservedAt, time.Now(), 90*time.Second) {
		return nil, nil
	}
	return &out, nil
}
