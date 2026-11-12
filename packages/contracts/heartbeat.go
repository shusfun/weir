package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

const HeartbeatProtocol = 2
const HeartbeatFullEvery = 5 * time.Minute
const HostSampleEvery = time.Minute

// HeartbeatDigest covers component and configuration observations. Sampling
// timestamps, resource counters and correlation IDs have their own cadence.
func HeartbeatDigest(h Heartbeat) (string, error) {
	h.Protocol, h.Digest, h.Full, h.Host, h.KernelEvents = 0, "", false, nil, nil
	if h.SelfUpdate != nil {
		copy := *h.SelfUpdate
		copy.At = time.Time{}
		h.SelfUpdate = &copy
	}
	if h.Firewall != nil {
		copy := *h.Firewall
		copy.Rules = append([]string(nil), h.Firewall.Rules...)
		sort.Strings(copy.Rules)
		h.Firewall = &copy
	}
	h.ListeningPorts = append([]ListeningPort(nil), h.ListeningPorts...)
	sort.Slice(h.ListeningPorts, func(i, j int) bool {
		a, b := h.ListeningPorts[i], h.ListeningPorts[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		return a.Process < b.Process
	})
	raw, err := json.Marshal(h)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
