package contracts

import "errors"

// WorkerBudget is host policy, never an instruction supplied by task text.
type WorkerBudget struct {
	Concurrency             int   `json:"concurrency"`
	BandwidthBytesPerSecond int64 `json:"bandwidth_bytes_per_second"`
	// CPUMaxPercent 是 CPU 密集任务的上限，按单核百分比计。0 表示使用默认值。
	// 心跳、配置同步和代理内核不使用这份预算。
	CPUMaxPercent int `json:"cpu_max_percent,omitempty"`
}

func DefaultWorkerBudget() WorkerBudget {
	return WorkerBudget{Concurrency: 1, BandwidthBytesPerSecond: 2 << 20, CPUMaxPercent: 20}
}
func (p WorkerBudget) Validate() error {
	if p.CPUMaxPercent == 0 {
		p.CPUMaxPercent = 20
	}
	if p.Concurrency < 1 || p.Concurrency > 32 || p.BandwidthBytesPerSecond < 64<<10 || p.BandwidthBytesPerSecond > 128<<20 || p.CPUMaxPercent < 1 || p.CPUMaxPercent > 100 {
		return errors.New("worker budget out of bounds")
	}
	return nil
}

func CPUHeavy(kind string) bool {
	switch kind {
	case TaskLayaClassify, TaskExternalConvert:
		return true
	}
	return false
}

func LargeDownload(kind string) bool {
	switch kind {
	case ActionAssetFetch, ActionReleaseReplace, TaskLayaPrepare, TaskLayaInstall, TaskCoreInstall:
		return true
	}
	return false
}
