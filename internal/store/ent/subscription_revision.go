package ent

import (
	"context"
	"errors"
	"strconv"

	"entgo.io/ent/dialect/sql"
)

// LockSubscriptionRevision fences imports against every dependency writer,
// including writers which do not hold the subscription profile application lock.
// The dependency triggers update this same row in their transaction.
func (tx *Tx) LockSubscriptionRevision(ctx context.Context) (string, error) {
	var rows sql.Rows
	err := tx.config.driver.Query(ctx, "UPDATE subscription_cache_state SET revision = revision WHERE id = 1 AND dirty = 0 RETURNING revision", []any{}, &rows)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", errors.New("subscription_data_busy")
	}
	var revision int64
	if err = rows.Scan(&revision); err != nil {
		return "", err
	}
	return strconv.FormatInt(revision, 10), rows.Err()
}
