package contracts

import (
	"encoding/json"
	"errors"
	"time"
)

type EmptyTask struct{}

type NodeProbeTask struct {
	Client NodeTestClient `json:"client"`
	UDP bool `json:"udp"`
	Credentials FetchSecrets `json:"credentials"`
	Kernels map[string]Kernel `json:"kernels"`
	Drivers map[string]string `json:"drivers"`
}

type SubscriptionProbeTask struct {
	Proxies []map[string]any `json:"proxies"`
	Target string `json:"target"`
	Kernels map[string]Kernel `json:"kernels"`
	Drivers map[string]string `json:"drivers"`
}

type NodeProbeCheck struct {
	Status string `json:"status"`
	Why string `json:"why,omitempty"`
	LatencyMS int64 `json:"latency_ms,omitempty"`
	Bytes int64 `json:"bytes,omitempty"`
}

type NodeProbeResult struct {
	TCP NodeProbeCheck `json:"tcp"`
	UDP NodeProbeCheck `json:"udp"`
}

type SubscriptionProbeResult struct {
	Checks map[string]NodeProbeCheck `json:"checks"`
}
type KernelTask struct {
	Kernel string `json:"kernel"`
}
type HostRepairTask struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type KernelInstallTask struct {
	Kernel   string         `json:"kernel"`
	Version  string         `json:"version"`
	Arch     string         `json:"arch"`
	Artifact KernelArtifact `json:"artifact"`
}
type RealityProbeTask struct {
	Targets []string `json:"targets"`
}
type LiveTask struct {
	Seconds int `json:"seconds"`
}
type PanelConfigureTask struct {
	Nonce string `json:"nonce"`
	Port  int    `json:"port"`
	HTTPS bool   `json:"https"`
}
type PanelCertificateTask struct {
	CertificateID int    `json:"cert_id"`
	IP            string `json:"ip"`
}
type ACMETask struct {
	Token            string `json:"token"`
	KeyAuthorization string `json:"key_auth"`
	Seconds          int    `json:"seconds"`
	Host             string `json:"host"`
}
type NodeTestTask struct {
	Config GateConfig     `json:"config"`
	Client NodeTestClient `json:"client"`
}
type OutboundProbeTask struct {
	Address    string            `json:"address"`
	Port       int               `json:"port"`
	PublicOnly bool              `json:"public_only"`
	Target     string            `json:"target"`
	Client     json.RawMessage   `json:"client"`
	Kernels    map[string]Kernel `json:"kernels"`
	Drivers    map[string]string `json:"drivers"`
}

// Secrets are transported in memory and excluded from progress and journals.
// The host encrypts the persisted task body before dispatch.
type FetchSecrets struct {
	URL           string `json:"url"`
	Authorization string `json:"authorization,omitempty"`
}
type SubscriptionFetchTask struct {
	Credentials FetchSecrets `json:"credentials"`
	UserAgent   string       `json:"user_agent,omitempty"`
	MaxBytes    int64        `json:"max_bytes"`
}
type SubscriptionFetchResult struct {
	Body        []byte `json:"body"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type,omitempty"`
	UserInfo    string `json:"user_info,omitempty"`
	Title       string `json:"title,omitempty"`
}
type ReleaseQueryTask struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag,omitempty"`
	PerPage    int    `json:"per_page,omitempty"` // >0 lists releases; zero queries a tag or latest
	MaxBytes   int64  `json:"max_bytes"`
}
type ExternalConvertTask struct {
	Credentials FetchSecrets `json:"credentials"`
	Method      string       `json:"method,omitempty"`
	Content     []byte       `json:"content"`
	ContentType string       `json:"content_type"`
	MaxBytes    int64        `json:"max_bytes"`
}
type TraceReadTask struct {
	TraceID string `json:"trace_id"`
	After   int64  `json:"after,omitempty"`
	Limit   int    `json:"limit"`
}

type TaskStageRecord struct {
	TaskID    string    `json:"task_id"`
	TraceID   string    `json:"trace_id"`
	Kind      string    `json:"kind"`
	Attempt   int       `json:"attempt"`
	Stage     string    `json:"stage"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	Outcome   string    `json:"outcome"`
	ErrorCode string    `json:"error_code,omitempty"`
	Origin    string    `json:"origin"`
}

type TraceReadResult struct {
	Records       []TaskStageRecord `json:"records"`
	CollectedAt   time.Time         `json:"collected_at"`
	Complete      bool              `json:"complete"`
	MissingReason string            `json:"missing_reason,omitempty"`
}
type LayaInstallTask struct {
	DeploymentVersion string          `json:"deployment_version"`
	Manifest          json.RawMessage `json:"manifest"`
}
type LayaDeploymentResult struct {
	State       string `json:"state"`
	Version     string `json:"version"`
	Model       string `json:"model"`
	ModelDigest string `json:"model_digest"`
	ImageDigest string `json:"image_digest"`
}
type LayaClassifyTask struct {
	ExpectedRuntimeVersion string                      `json:"expected_runtime_version,omitempty"`
	Model                  string                      `json:"model"`
	State                  json.RawMessage             `json:"state"`
	Questions              map[string]DecisionQuestion `json:"questions"`
}
type DecisionQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions,omitempty"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
	Ordinal      *bool           `json:"ordinal,omitempty"`
}

func NewTaskRequest(kind string, version int) (any, error) {
	if version != TaskVersion {
		return nil, errors.New("unsupported task version")
	}
	switch kind {
	case TaskNodeProbe:
		return new(NodeProbeTask), nil
	case TaskSubscriptionProbe:
		return new(SubscriptionProbeTask), nil
	case TaskCoreInstall:
		return new(KernelInstallTask), nil
	case ActionAssetFetch:
		return new(AssetFetch), nil
	case ActionSourceProbe:
		return new(SourceProbe), nil
	case ActionNodeTest:
		return new(NodeTestTask), nil
	case ActionOutboundProbe, ActionChainProbe:
		return new(OutboundProbeTask), nil
	case ActionRealityProbe:
		return new(RealityProbeTask), nil
	case ActionPanelConfigure:
		return new(PanelConfigureTask), nil
	case ActionPanelCert:
		return new(PanelCertificateTask), nil
	case ActionPanelDiscover, ActionPanelRemove, ActionUninstall, TaskLayaRemove:
		return new(EmptyTask), nil
	case ActionACMEHTTP01:
		return new(ACMETask), nil
	case ActionDiagnostics, ActionCoreRestart:
		return new(KernelTask), nil
	case ActionHostRepair:
		return new(HostRepairTask), nil
	case ActionLiveStart:
		return new(LiveTask), nil
	case ActionReleaseReplace:
		return new(ReleaseTarget), nil
	case TaskSubscriptionFetch:
		return new(SubscriptionFetchTask), nil
	case TaskReleaseQuery:
		return new(ReleaseQueryTask), nil
	case TaskExternalConvert:
		return new(ExternalConvertTask), nil
	case TaskTraceRead:
		return new(TraceReadTask), nil
	case TaskLayaInstall:
		return new(LayaInstallTask), nil
	case TaskLayaClassify:
		return new(LayaClassifyTask), nil
	default:
		return nil, errors.New("unknown task type")
	}
}
