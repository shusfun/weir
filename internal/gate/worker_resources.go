package gate

import (
	"github.com/shusfun/weir/packages/contracts"
	"net/http"
)

func (a *Agent) currentWorkerBudget() contracts.WorkerBudget {
	a.workerBudgetMu.RLock()
	defer a.workerBudgetMu.RUnlock()
	if a.workerBudget.Validate() != nil {
		return contracts.DefaultWorkerBudget()
	}
	return a.workerBudget
}

func (a *Agent) setWorkerBudget(p contracts.WorkerBudget) error {
	if err := p.Validate(); err != nil {
		return err
	}
	a.workerBudgetMu.Lock()
	a.workerBudget = p
	a.workerBudgetMu.Unlock()
	a.workerPacer.SetRate(p.BandwidthBytesPerSecond)
	return nil
}

func (a *Agent) workerHTTP(client *http.Client) *http.Client {
	a.workerPacer.SetRate(a.currentWorkerBudget().BandwidthBytesPerSecond)
	return a.workerPacer.Client(client)
}

func sampleWorkerResources() contracts.WorkerResources {
	r := contracts.WorkerResources{Concurrency: 1}
	h := sampleHost()
	if h == nil {
		return r
	}
	if h.CPUPercent != nil && *h.CPUPercent >= 0 && *h.CPUPercent <= 100 {
		v := *h.CPUPercent / 100
		r.Load = &v
	}
	if h.MemTotal != nil && h.MemUsed != nil && *h.MemUsed <= *h.MemTotal {
		v := *h.MemTotal - *h.MemUsed
		r.AvailableMemory = &v
	}
	if h.DiskTotal != nil && h.DiskUsed != nil && *h.DiskUsed <= *h.DiskTotal {
		v := *h.DiskTotal - *h.DiskUsed
		r.AvailableDisk = &v
	}
	return r
}
