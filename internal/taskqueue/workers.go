package taskqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/shusfun/weir/packages/contracts"
)

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
	_, err = q.DB.ExecContext(ctx, `INSERT INTO worker_capabilities(gate_id,hash,document) VALUES($1,$2,$3) ON CONFLICT(gate_id) DO UPDATE SET hash=excluded.hash,document=excluded.document WHERE worker_capabilities.hash<>excluded.hash`, gate, hex.EncodeToString(sum[:]), string(b))
	return err
}
func (q *Queue) Capabilities(ctx context.Context, gate int) (c contracts.CapabilityDocument, err error) {
	var raw string
	err = q.DB.QueryRowContext(ctx, `SELECT document FROM worker_capabilities WHERE gate_id=$1`, gate).Scan(&raw)
	if err != nil {
		return c, err
	}
	err = contracts.DecodeStrict([]byte(raw), &c, 256<<10)
	if err == nil {
		err = c.Validate()
	}
	return c, err
}
