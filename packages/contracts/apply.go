package contracts

import "time"

// 配置应用方式。断连数用指针：没有读到是 nil，页面显示「未知」，不能写成 0。
const (
	ApplyUnchanged = "unchanged"
	ApplyHotUsers  = "hot_users"
	ApplyRestart   = "restart"
)

// ApplyReport 是 Gate 一次应用的结果，不含凭据和配置正文。
type ApplyReport struct {
	Mode        string          `json:"mode"`
	Reason      string          `json:"reason,omitempty"`
	PID         map[string]int  `json:"pid,omitempty"`
	Disconnects map[string]*int `json:"disconnects,omitempty"`
	Events      []KernelEvent   `json:"events,omitempty"`
	Revision    int64           `json:"revision,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"`
}

// KernelEvent 是 helper 记下的一次内核启停。PID 或断连数读不到时字段省略。
type KernelEvent struct {
	At          time.Time `json:"at"`
	Kernel      string    `json:"kernel"`
	Kind        string    `json:"kind"` // start / stop / crash / recover
	PID         *int      `json:"pid,omitempty"`
	Disconnects *int      `json:"disconnects,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	TraceID     string    `json:"trace_id,omitempty"`
}

// GateExecution 是不进入内核配置正文的执行状态。
type GateExecution struct {
	Seq             int64                 `json:"seq"`
	Budgets         map[string]GateBudget `json:"budgets,omitempty"`
	ForceDisconnect *bool                 `json:"force_disconnect,omitempty"`
}
