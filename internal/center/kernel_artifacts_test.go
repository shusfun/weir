package center

import (
	"strings"
	"testing"

	"github.com/shusfun/weir/packages/contracts"
)

func TestGateKernelManifestDoesNotRequireCenterBinary(t *testing.T) {
	s, admin := newInstalledServer(t)
	g := pairFakeGate(t, admin)
	heartbeat(t, g, contracts.Heartbeat{Version: "test", Arch: "amd64"})
	artifact := contracts.KernelArtifact{Schema: 1, Kernel: "xray", Version: "26.3.27", Arch: "amd64", URLs: []string{"https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip"}, SHA256: strings.Repeat("a", 64), Size: 1234}
	if err := s.saveKernelArtifact(t.Context(), artifact); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Tasks.SetCapabilities(t.Context(), int(g.id), contracts.CapabilityDocument{Protocol: contracts.AgentProtocol, Version: "test", Arch: "amd64", Tasks: map[string]int{contracts.TaskCoreInstall: 1}}); err != nil {
		t.Fatal(err)
	}
	gate, err := s.Store.Ent.Gate.UpdateOneID(int(g.id)).SetKernelPins(map[string]string{"xray": artifact.Version}).Save(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	refs, profiles, err := s.gateKernels(t.Context(), gate, map[string]bool{"xray": true})
	if err != nil || refs["xray"].Artifact == nil || refs["xray"].SHA256 != "" || profiles["xray"] == "" {
		t.Fatalf("manifest-only configuration: %+v %v", refs, err)
	}
	if _, _, err := s.kernelCache().Get("xray", artifact.Version, "amd64"); err == nil {
		t.Fatal("metadata resolution unexpectedly downloaded binary")
	}
	changed := artifact
	changed.SHA256 = strings.Repeat("b", 64)
	if err := s.saveKernelArtifact(t.Context(), changed); err == nil {
		t.Fatal("release silently replaced a pinned digest")
	}
	versions, err := s.kernelVersions(t.Context(), "xray")
	if err != nil || len(versions) != 1 || versions[0] != artifact.Version {
		t.Fatalf("catalog missing manifest-only version: %v %v", versions, err)
	}
	if err := s.saveKernelDefault(t.Context(), "xray", artifact.Version, 0, "", "fixture"); err != nil {
		t.Fatal("manifest-only version cannot become default", err)
	}
}
