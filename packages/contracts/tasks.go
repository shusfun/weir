package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"time"
)

const (
	AgentProtocol         = 2
	TaskVersion           = 1
	TaskLease             = 60 * time.Second
	TaskRenewEvery        = 20 * time.Second
	MaxTaskPayload        = 1 << 20
	MaxTaskResult         = 16 << 20
	TaskSubscriptionFetch = "subscription.fetch"
	TaskReleaseQuery      = "release.query"
	TaskExternalConvert   = "subscription.convert"
	TaskTraceRead         = "trace.read"
	TaskLayaInstall       = "laya.install"
	TaskLayaRemove        = "laya.remove"
	TaskLayaClassify      = "laya.classify"
	TaskCoreInstall       = "core.install"
	TaskNodeProbe         = "node.probe"
	TaskSubscriptionProbe = "subscription.probe"
)

type TaskError struct {
	Code      string `json:"code"`
	Origin    string `json:"origin"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message,omitempty"`
}

// Error deliberately excludes Message: upstream text is not safe to log.
func (e *TaskError) Error() string { return e.Code }

var taskErrorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}$`)
var taskErrorOrigin = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.*()/\-]{0,255}$`)
var componentName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,99}$`)

func (e *TaskError) Validate() error {
	if e == nil || !taskErrorCode.MatchString(e.Code) || e.Message != "" || e.Origin != "" && !taskErrorOrigin.MatchString(e.Origin) {
		return errors.New("invalid structured task error")
	}
	return nil
}

type TaskRequirements struct {
	TargetGateID    int    `json:"target_gate_id,omitempty"`
	PreferredGateID int    `json:"preferred_gate_id,omitempty"`
	Region          string `json:"region,omitempty"`
	Source          string `json:"source,omitempty"`
	MemoryBytes     uint64 `json:"memory_bytes,omitempty"`
	DiskBytes       uint64 `json:"disk_bytes,omitempty"`
}

type Task struct {
	ID             string           `json:"id"`
	Kind           string           `json:"kind"`
	Version        int              `json:"version"`
	TraceID        string           `json:"trace_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Requirements   TaskRequirements `json:"requirements"`
	Payload        json.RawMessage  `json:"payload"`
	Attempt        int              `json:"attempt"`
	LeaseToken     string           `json:"lease_token"`
	LeaseUntil     time.Time        `json:"lease_until"`
	Deadline       time.Time        `json:"deadline"`
}

type TaskUpdate struct {
	Attempt    int             `json:"attempt"`
	LeaseToken string          `json:"lease_token"`
	Progress   *TaskProgress   `json:"progress,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *TaskError      `json:"error,omitempty"`
}

type TaskProgress struct {
	Stage string `json:"stage"`
	Done  int64  `json:"done"`
	Total *int64 `json:"total,omitempty"`
}

func (p TaskProgress) Validate() error {
	switch p.Stage {
	case "preparing", "downloading", "verifying", "executing", "finalizing":
	default:
		return errors.New("unknown progress stage")
	}
	if p.Done < 0 || p.Total != nil && (*p.Total < 0 || p.Done > *p.Total) {
		return errors.New("invalid progress bounds")
	}
	return nil
}

type CapabilityDocument struct {
	Protocol                int                            `json:"protocol"`
	Version                 string                         `json:"version"`
	Arch                    string                         `json:"arch"`
	OS                      string                         `json:"os,omitempty"`
	Tasks                   map[string]int                 `json:"tasks"`
	Region                  string                         `json:"region,omitempty"`
	ASN                     *uint32                        `json:"asn,omitempty"`
	BandwidthBytesPerSecond *uint64                        `json:"bandwidth_bytes_per_second,omitempty"`
	Sources                 map[string]bool                `json:"sources,omitempty"`
	Components              map[string]ComponentCapability `json:"components,omitempty"`
}

type ComponentCapability struct {
	Version string            `json:"version"`
	Models  map[string]string `json:"models,omitempty"`
}

type WorkerResources struct {
	SourcesObservedAt *time.Time                      `json:"sources_observed_at,omitempty"`
	Components        map[string]ComponentObservation `json:"components,omitempty"`
	Load              *float64                        `json:"load,omitempty"`
	AvailableMemory   *uint64                         `json:"available_memory,omitempty"`
	AvailableDisk     *uint64                         `json:"available_disk,omitempty"`
	Concurrency       int                             `json:"concurrency"`
}

// ComponentObservation is a bounded runtime observation, separate from stable
// capabilities. A running process does not imply a successful inference.
type ComponentObservation struct {
	State      string    `json:"state"`
	Code       string    `json:"code,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Version    string    `json:"version,omitempty"`
	Model      string    `json:"model,omitempty"`
	Digest     string    `json:"digest,omitempty"`
}

func (o ComponentObservation) Validate() error {
	switch o.State {
	case "running", "stopped", "available", "unknown":
	default:
		return errors.New("invalid component state")
	}
	if o.ObservedAt.IsZero() || len(o.Version) > 100 || len(o.Model) > 100 || len(o.Digest) > 200 || o.Code != "" && !taskErrorCode.MatchString(o.Code) {
		return errors.New("invalid component observation")
	}
	return nil
}

func (r WorkerResources) Validate() error {
	if r.Concurrency < 1 || r.Concurrency > 32 || r.Load != nil && (math.IsNaN(*r.Load) || math.IsInf(*r.Load, 0) || *r.Load < 0 || *r.Load > 1) {
		return errors.New("invalid worker resource observation")
	}
	if r.SourcesObservedAt != nil && r.SourcesObservedAt.IsZero() {
		return errors.New("invalid source observation timestamp")
	}
	if len(r.Components) > 4 {
		return errors.New("too many component observations")
	}
	for name, observation := range r.Components {
		if name != "laya" && name != "xray" && name != "sing-box" || observation.Validate() != nil {
			return errors.New("invalid worker component observation")
		}
	}
	return nil
}

type AgentPoll struct {
	Protocol       int                 `json:"protocol"`
	ConfigRevision int64               `json:"config_revision"`
	ExecutionSeq   int64               `json:"execution_seq,omitempty"`
	Capabilities   *CapabilityDocument `json:"capabilities,omitempty"`
	Resources      WorkerResources     `json:"resources"`
}

type AgentPollReply struct {
	WorkerBudget      *WorkerBudget `json:"worker_budget,omitempty"`
	HeartbeatProtocol int           `json:"heartbeat_protocol,omitempty"`
	PayloadEncodings  []string      `json:"payload_encodings,omitempty"`
	MeterProtocol     int           `json:"meter_protocol,omitempty"`
	Protocol          int           `json:"protocol"`
	Config            *GateConfig   `json:"config,omitempty"`
	Tasks             []Task        `json:"tasks,omitempty"`
	Cancel            []string      `json:"cancel,omitempty"`
}

// DecodeStrict bounds both the envelope and trailing input. RawMessage is only
// used at the wire boundary; a registered concrete request must be decoded next.
func DecodeStrict(raw []byte, out any, limit int) error {
	if len(raw) == 0 || len(raw) > limit {
		return errors.New("invalid payload size")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON input")
	}
	return nil
}

func (c CapabilityDocument) Validate() error {
	if c.Protocol != AgentProtocol || c.Version == "" || len(c.Version) > 100 || (c.Arch != "amd64" && c.Arch != "arm64") || len(c.Tasks) > 64 || len(c.Sources) > 32 || len(c.Components) > 32 || len(c.Region) > 100 {
		return errors.New("invalid capability document")
	}
	if c.OS != "" && c.OS != "linux" && c.OS != "darwin" && c.OS != "windows" {
		return errors.New("invalid capability operating system")
	}
	for source := range c.Sources {
		if !taskErrorCode.MatchString(source) {
			return errors.New("invalid source capability")
		}
	}
	for name, component := range c.Components {
		if !componentName.MatchString(name) || len(component.Version) > 100 || len(component.Models) > 32 {
			return errors.New("invalid component capability")
		}
		for model, version := range component.Models {
			if len(model) < 1 || len(model) > 100 || len(version) > 200 {
				return errors.New("invalid model capability")
			}
		}
	}
	for k, v := range c.Tasks {
		if v != TaskVersion || !KnownTask(k) {
			return fmt.Errorf("unsupported task capability: %s", k)
		}
	}
	return nil
}

func KnownTask(kind string) bool { _, err := NewTaskRequest(kind, TaskVersion); return err == nil }

// Relocatable only applies to work without effects tied to a particular VPS.
func Relocatable(kind string) bool {
	switch kind {
	case TaskSubscriptionFetch, TaskReleaseQuery, TaskExternalConvert, ActionSourceProbe:
		return true
	default:
		return false
	}
}

func ValidateTaskPayload(kind string, version int, raw json.RawMessage) error {
	v, err := NewTaskRequest(kind, version)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("task payload cannot be null")
	}
	if err := DecodeStrict(raw, v, MaxTaskPayload); err != nil {
		return err
	}
	return validateTaskRequest(v)
}
