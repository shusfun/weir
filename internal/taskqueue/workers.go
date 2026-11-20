package taskqueue

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/shusfun/weir/internal/store/ent/workercapability"
	"github.com/shusfun/weir/packages/contracts"
)

// ErrInvalidCapabilities identifies an unusable cached document, not a storage
// failure. A freshly validated report may replace it without trusting its fields.
var ErrInvalidCapabilities = errors.New("invalid cached worker capabilities")

func (q *Queue) SetCapabilities(ctx context.Context, gate int, c contracts.CapabilityDocument) error {
	if gate <= 0 {
		return errors.New("invalid worker")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(b) > 256<<10 {
		return errors.New("capability document too large")
	}
	sum := sha256.Sum256(b)
	// 文档未变时不改写行，避免每次心跳都产生写入。
	err = q.Ent.WorkerCapability.Create().SetID(gate).SetHash(hex.EncodeToString(sum[:])).SetDocument(string(b)).
		OnConflict(
			entsql.ConflictColumns(workercapability.FieldID),
			entsql.UpdateWhere(entsql.ColumnsNEQ(entsql.Dialect(q.Dialect).Table(workercapability.Table).C(workercapability.FieldHash), entsql.Dialect(q.Dialect).Table("excluded").C(workercapability.FieldHash))),
		).
		UpdateNewValues().Exec(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (q *Queue) Capabilities(ctx context.Context, gate int) (contracts.CapabilityDocument, error) {
	c, err := q.Ent.WorkerCapability.Get(ctx, gate)
	if err != nil {
		return contracts.CapabilityDocument{}, notFound(err)
	}
	return decodeCapabilities(c.Document)
}

func decodeCapabilities(raw string) (contracts.CapabilityDocument, error) {
	var c contracts.CapabilityDocument
	err := contracts.DecodeStrict([]byte(raw), &c, 256<<10)
	if err == nil {
		err = c.Validate()
	}
	if err != nil {
		return contracts.CapabilityDocument{}, fmt.Errorf("%w: %w", ErrInvalidCapabilities, err)
	}
	return c, nil
}
