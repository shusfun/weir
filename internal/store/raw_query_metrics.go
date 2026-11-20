package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
)

func (d measuredTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	defer recordQuery(ctx, start)
	tx, ok := d.Tx.(entsql.ExecQuerier)
	if !ok {
		return nil, fmt.Errorf("transaction %T does not support raw queries", d.Tx)
	}
	return tx.QueryContext(ctx, query, args...)
}
