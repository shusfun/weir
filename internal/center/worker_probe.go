package center

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/shusfun/weir/internal/taskqueue"
	"github.com/shusfun/weir/packages/contracts"
)

func (s *Server) validateProbeResult(ctx context.Context, record taskqueue.Record, raw json.RawMessage) error {
	valid := func(c contracts.NodeProbeCheck, udp bool) bool {
		return (c.Status == "passed" || c.Status == "failed" || c.Status == "unknown" || udp && c.Status == "not_applicable") && c.LatencyMS >= 0 && c.LatencyMS <= 300000 && c.Bytes >= 0 && c.Bytes <= 256<<10 && (c.Why == "" || c.Why == "probe_certificate_failed" || c.Why == "probe_authentication_failed" || c.Why == "probe_port_refused" || c.Why == "probe_timeout" || c.Why == "probe_address_rejected" || c.Why == "probe_connection_failed" || c.Why == "probe_execution_failed")
	}
	if record.Task.Kind == contracts.TaskNodeProbe {
		var result contracts.NodeProbeResult
		if contracts.DecodeStrict(raw, &result, 64<<10) != nil || !valid(result.TCP, false) || !valid(result.UDP, true) {
			return errors.New("probe_result_invalid")
		}
		return nil
	}
	var result contracts.SubscriptionProbeResult
	if contracts.DecodeStrict(raw, &result, 256<<10) != nil {
		return errors.New("probe_result_invalid")
	}
	body, err := s.Crypto.Decrypt(record.Ciphertext)
	if err != nil {
		return err
	}
	var request contracts.SubscriptionProbeTask
	if err = contracts.DecodeStrict([]byte(body), &request, contracts.MaxTaskPayload); err != nil {
		return err
	}
	if len(result.Checks) != len(request.Proxies) {
		return errors.New("probe_result_members_invalid")
	}
	for _, proxy := range request.Proxies {
		name, _ := proxy["name"].(string)
		check, ok := result.Checks[name]
		if !ok || !valid(check, false) {
			return errors.New("probe_result_members_invalid")
		}
	}
	return nil
}
