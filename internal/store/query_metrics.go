package store

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	"sync/atomic"
	"time"
)

type queryMetricsKey struct{}

// QueryMetrics contains counts and time only; SQL and parameters are never kept.
// Duration includes connection acquisition and driver execution, not row iteration.
type QueryMetrics struct {
	Count           atomic.Int64
	Nanoseconds     atomic.Int64
	LockNanoseconds atomic.Int64
}

func ObserveQueries(ctx context.Context) (context.Context, *QueryMetrics) {
	m := &QueryMetrics{}
	return context.WithValue(ctx, queryMetricsKey{}, m), m
}
func recordQuery(ctx context.Context, started time.Time) {
	if m, ok := ctx.Value(queryMetricsKey{}).(*QueryMetrics); ok {
		m.Count.Add(1)
		m.Nanoseconds.Add(time.Since(started).Nanoseconds())
	}
}
func recordLock(ctx context.Context, started time.Time) {
	if m, ok := ctx.Value(queryMetricsKey{}).(*QueryMetrics); ok {
		m.LockNanoseconds.Add(time.Since(started).Nanoseconds())
	}
}

type measuredDriver struct{ dialect.Driver }

func (d measuredDriver) Exec(ctx context.Context, q string, args, v any) error {
	start := time.Now()
	defer recordQuery(ctx, start)
	return d.Driver.Exec(ctx, q, args, v)
}
func (d measuredDriver) Query(ctx context.Context, q string, args, v any) error {
	start := time.Now()
	defer recordQuery(ctx, start)
	return d.Driver.Query(ctx, q, args, v)
}
func (d measuredDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return measuredTx{tx}, nil
}
func (d measuredDriver) BeginTx(ctx context.Context, opts *sql.TxOptions) (dialect.Tx, error) {
	if driver, ok := d.Driver.(interface {
		BeginTx(context.Context, *sql.TxOptions) (dialect.Tx, error)
	}); ok {
		tx, err := driver.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return measuredTx{tx}, nil
	}
	return d.Tx(ctx)
}

type measuredTx struct{ dialect.Tx }

func (d measuredTx) Exec(ctx context.Context, q string, args, v any) error {
	start := time.Now()
	defer recordQuery(ctx, start)
	return d.Tx.Exec(ctx, q, args, v)
}
func (d measuredTx) Query(ctx context.Context, q string, args, v any) error {
	start := time.Now()
	defer recordQuery(ctx, start)
	return d.Tx.Query(ctx, q, args, v)
}
