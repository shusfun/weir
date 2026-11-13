package gate

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/shusfun/weir/internal/kernelconf"
	"github.com/shusfun/weir/internal/nodeprobe"
	"github.com/shusfun/weir/packages/contracts"
)

func (a *Agent) probeTask(ctx context.Context, task contracts.Task) (json.RawMessage, error) {
	var cfg contracts.GateConfig
	var node contracts.NodeProbeTask
	var batch contracts.SubscriptionProbeTask
	if task.Kind == contracts.TaskNodeProbe {
		if err := contracts.DecodeStrict(task.Payload, &node, contracts.MaxTaskPayload); err != nil {
			return nil, err
		}
		cfg.Kernels, cfg.Drivers = node.Kernels, node.Drivers
	} else {
		if err := contracts.DecodeStrict(task.Payload, &batch, contracts.MaxTaskPayload); err != nil {
			return nil, err
		}
		cfg.Kernels, cfg.Drivers = batch.Kernels, batch.Drivers
	}
	k, ok := cfg.Kernels["mihomo"]
	if !ok || len(cfg.Kernels) != 1 {
		return nil, errors.New("probe_kernel_missing")
	}
	if err := a.ensureKernels(ctx, cfg); err != nil {
		return nil, err
	}
	d, err := kernelconf.DriverFor(cfg, "mihomo")
	if err != nil || d == nil {
		return nil, errors.New("probe_driver_invalid")
	}
	finish := beginTaskStage(ctx, "node.probe", "gate.probeTask")
	defer func() { finish(err) }()
	bin := filepath.Join(a.P.KernelsDir, "mihomo-"+k.Version)
	if task.Kind == contracts.TaskNodeProbe {
		result, e := nodeprobe.Run(ctx, nodeprobe.Options{Binary: bin, Run: d.Run, Check: d.Check, Client: node.Client, UDP: node.UDP, Target: node.Credentials.URL, Token: node.Credentials.Authorization, PublicOnly: true})
		// A failed connectivity check is a valid result, distinct from a failed
		// worker execution. Raw child-process output never crosses this boundary.
		if ctx.Err() != nil {
			err = ctx.Err()
			return nil, err
		}
		if e != nil && result.TCP.Status == "unknown" {
			result.TCP.Why = "probe_execution_failed"
		}
		result.TCP.Why = probeReason(result.TCP.Why)
		result.UDP.Why = probeReason(result.UDP.Why)
		return json.Marshal(result)
	}
	checks, e := nodeprobe.Batch(ctx, nodeprobe.BatchOptions{Binary: bin, Run: d.Run, Check: d.Check, Proxies: batch.Proxies, Target: batch.Target, PublicOnly: true})
	if e != nil {
		err = errors.New("probe_execution_failed")
		return nil, err
	}
	for name, check := range checks {
		check.Why = probeReason(check.Why)
		checks[name] = check
	}
	return json.Marshal(contracts.SubscriptionProbeResult{Checks: checks})
}

func probeReason(raw string) string {
	switch {
	case raw == "":
		return ""
	case strings.Contains(raw, "证书"), strings.Contains(raw, "certificate"), strings.Contains(raw, "x509"):
		return "probe_certificate_failed"
	case strings.Contains(raw, "认证"), strings.Contains(raw, "authentication"):
		return "probe_authentication_failed"
	case strings.Contains(raw, "refused"):
		return "probe_port_refused"
	case strings.Contains(raw, "timeout"), strings.Contains(raw, "deadline"), strings.Contains(raw, "超时"):
		return "probe_timeout"
	case strings.Contains(raw, "public_address_private"), strings.Contains(raw, "public_url_private"):
		return "probe_address_rejected"
	default:
		return "probe_connection_failed"
	}
}
