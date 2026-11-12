package taskqueue

import (
	"context"
	"errors"
	"time"
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
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, entry := range entries {
		if entry.GateID < 1 || len(entry.Route) > 200 || entry.Requests < 0 || entry.Received < 0 || entry.Sent < 0 || entry.Errors < 0 {
			return errors.New("invalid communication entry")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO worker_communication(bucket,gate_id,route,requests,received,sent,errors) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(bucket,gate_id,route) DO UPDATE SET requests=worker_communication.requests+excluded.requests,received=worker_communication.received+excluded.received,sent=worker_communication.sent+excluded.sent,errors=worker_communication.errors+excluded.errors`, entry.Bucket.UTC().Truncate(time.Minute).UnixMilli(), entry.GateID, entry.Route, entry.Requests, entry.Received, entry.Sent, entry.Errors); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (q *Queue) Communication(ctx context.Context, since time.Time) ([]Communication, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT gate_id,route,sum(requests),sum(received),sum(sent),sum(errors) FROM worker_communication WHERE bucket>=$1 GROUP BY gate_id,route ORDER BY gate_id,route LIMIT 10000`, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Communication{}
	for rows.Next() {
		var entry Communication
		if err = rows.Scan(&entry.GateID, &entry.Route, &entry.Requests, &entry.Received, &entry.Sent, &entry.Errors); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}
