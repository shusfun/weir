package taskqueue

import (
	"context"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/workercommunication"
)

type Communication struct {
	Bucket   time.Time `json:"bucket"`
	GateID   int       `json:"gate_id"`
	Route    string    `json:"route"`
	Requests int64     `json:"requests"`
	Received int64     `json:"received_body_bytes"`
	Sent     int64     `json:"sent_body_bytes"`
	Errors   int64     `json:"errors"`
}

func (q *Queue) AddCommunication(ctx context.Context, entries []Communication) error {
	if len(entries) > 10000 {
		return errors.New("communication batch too large")
	}
	for _, entry := range entries {
		if entry.GateID < 1 || len(entry.Route) > 200 || entry.Requests < 0 || entry.Received < 0 || entry.Sent < 0 || entry.Errors < 0 {
			return errors.New("invalid communication entry")
		}
	}
	return q.withTx(ctx, func(tx *ent.Tx) error {
		for _, entry := range entries {
			err := tx.WorkerCommunication.Create().SetID(entry.Bucket.UTC().Truncate(time.Minute).UnixMilli()).SetGateID(entry.GateID).
				SetRoute(entry.Route).SetRequests(entry.Requests).SetReceived(entry.Received).SetSent(entry.Sent).SetErrors(entry.Errors).
				OnConflictColumns(workercommunication.FieldID, workercommunication.FieldGateID, workercommunication.FieldRoute).
				Update(func(u *ent.WorkerCommunicationUpsert) {
					u.AddRequests(entry.Requests).AddReceived(entry.Received).AddSent(entry.Sent).AddErrors(entry.Errors)
				}).Exec(ctx)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *Queue) Communication(ctx context.Context, since time.Time) ([]Communication, error) {
	var rows []struct {
		GateID   int    `json:"gate_id"`
		Route    string `json:"route"`
		Requests int64  `json:"requests"`
		Received int64  `json:"received"`
		Sent     int64  `json:"sent"`
		Errors   int64  `json:"errors"`
	}
	err := q.Ent.WorkerCommunication.Query().Where(workercommunication.IDGTE(since.UnixMilli())).Modify(func(s *entsql.Selector) {
		group := []string{s.C(workercommunication.FieldGateID), s.C(workercommunication.FieldRoute)}
		sum := func(field string) string { return entsql.As(entsql.Sum(s.C(field)), field) }
		s.Select(append(group, sum(workercommunication.FieldRequests), sum(workercommunication.FieldReceived),
			sum(workercommunication.FieldSent), sum(workercommunication.FieldErrors))...).
			GroupBy(group...).OrderBy(group...).Limit(10000)
	}).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]Communication, 0, len(rows))
	for _, r := range rows {
		out = append(out, Communication{GateID: r.GateID, Route: r.Route, Requests: r.Requests, Received: r.Received, Sent: r.Sent, Errors: r.Errors})
	}
	return out, nil
}
