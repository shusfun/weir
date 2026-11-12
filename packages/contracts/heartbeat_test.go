package contracts

import (
	"testing"
	"time"
)

func TestHeartbeatDigestIgnoresSamplingAndOrdering(t *testing.T) {
	h := Heartbeat{Version: "v1", Arch: "amd64", ListeningPorts: []ListeningPort{{Port: 443, Proto: "tcp"}, {Port: 53, Proto: "udp"}}, Kernels: map[string]string{"xray": "v1"}, Host: &HostSample{ObservedAt: time.Now()}, SelfUpdate: &SelfUpdateStatus{Version: "v2", State: "downloading", Done: 10, At: time.Now()}}
	digest, err := HeartbeatDigest(h)
	if err != nil {
		t.Fatal(err)
	}
	copy := h
	copy.Protocol, copy.Full, copy.Digest = 2, true, "untrusted"
	copy.Host = &HostSample{ObservedAt: time.Now().Add(time.Hour), ObservationID: "another"}
	copy.SelfUpdate = &SelfUpdateStatus{Version: "v2", State: "downloading", Done: 10, At: time.Now().Add(time.Hour)}
	copy.ListeningPorts = []ListeningPort{h.ListeningPorts[1], h.ListeningPorts[0]}
	other, err := HeartbeatDigest(copy)
	if err != nil || other != digest {
		t.Fatal("volatile fields changed snapshot digest", err)
	}
	copy.SelfUpdate.Done++
	other, _ = HeartbeatDigest(copy)
	if other == digest {
		t.Fatal("actual progress change lost")
	}
	if h.ListeningPorts[0].Port != 443 {
		t.Fatal("digest mutated its input")
	}
}
