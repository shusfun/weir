package gate

import (
	"sync"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

type heartbeatState struct {
	mu               sync.Mutex
	digest           string
	fullAt, sampleAt time.Time
}

func (h *heartbeatState) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.digest = ""
	h.fullAt = time.Time{}
	h.sampleAt = time.Time{}
}

func (h *heartbeatState) prepare(snapshot contracts.Heartbeat, now time.Time, fullEvery, sampleEvery time.Duration) (contracts.Heartbeat, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Bounded host policy; tests can drive time without changing the cadence.
	if fullEvery < time.Minute || fullEvery > time.Hour {
		fullEvery = contracts.HeartbeatFullEvery
	}
	if sampleEvery < 30*time.Second || sampleEvery > 15*time.Minute {
		sampleEvery = contracts.HostSampleEvery
	}
	digest, err := contracts.HeartbeatDigest(snapshot)
	if err != nil {
		return contracts.Heartbeat{}, err
	}
	full := h.digest != digest || now.Sub(h.fullAt) >= fullEvery
	wire := contracts.Heartbeat{Protocol: contracts.HeartbeatProtocol, Digest: digest, Full: full}
	if full {
		wire = snapshot
		wire.Protocol, wire.Digest, wire.Full = contracts.HeartbeatProtocol, digest, true
	}
	wire.Host = nil
	if now.Sub(h.sampleAt) >= sampleEvery {
		wire.Host = snapshot.Host
	}
	wire.KernelEvents = snapshot.KernelEvents
	return wire, nil
}

func (h *heartbeatState) acknowledge(sent contracts.Heartbeat, reply contracts.HeartbeatReply, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if reply.NeedFull {
		h.digest = ""
		return
	}
	if sent.Protocol != contracts.HeartbeatProtocol || reply.Digest != sent.Digest {
		return
	}
	h.digest = sent.Digest
	if sent.Full {
		h.fullAt = now
	}
	if sent.Host != nil {
		h.sampleAt = now
	}
}
