// Package contracts defines the versioned control/agent wire protocol.
package contracts

import "time"

const Version = 1
const BudgetBlock int64 = 32 * 1024 * 1024

type Task struct {
	ID        int            `json:"id"`
	DeviceID  int            `json:"device_id"`
	Operation string         `json:"operation"`
	Revision  int64          `json:"revision"`
	Payload   map[string]any `json:"payload"`
	ExpiresAt time.Time      `json:"expires_at"`
}
type Result struct {
	TaskID   int            `json:"task_id"`
	Revision int64          `json:"revision"`
	Status   string         `json:"status"`
	Data     map[string]any `json:"data"`
}
type Meter struct {
	GrantID  int    `json:"grant_id"`
	Sequence int64  `json:"sequence"`
	Epoch    string `json:"epoch"`
	Used     int64  `json:"used"`
}
type Budget struct {
	GrantID   int   `json:"grant_id"`
	Allocated int64 `json:"allocated"`
	Expires   int64 `json:"expires"`
	Enabled   bool  `json:"enabled"`
}
