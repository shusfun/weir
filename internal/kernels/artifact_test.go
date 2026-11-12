package kernels

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/packages/contracts"
)

func artifactELF(machine uint16) []byte {
	body := make([]byte, 120)
	copy(body, []byte{'\x7f', 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(body[16:], 2)
	binary.LittleEndian.PutUint16(body[18:], machine)
	binary.LittleEndian.PutUint32(body[20:], 1)
	binary.LittleEndian.PutUint64(body[32:], 64)
	binary.LittleEndian.PutUint16(body[52:], 64)
	binary.LittleEndian.PutUint16(body[54:], 56)
	binary.LittleEndian.PutUint16(body[56:], 1)
	binary.LittleEndian.PutUint32(body[64:], 1)
	binary.LittleEndian.PutUint32(body[68:], 5)
	binary.LittleEndian.PutUint64(body[96:], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[104:], uint64(len(body)))
	return body
}

func artifactFor(kernel, version, arch string, body []byte) contracts.KernelArtifact {
	return contracts.KernelArtifact{Schema: 1, Kernel: kernel, Version: version, Arch: arch, URLs: []string{"https://github.com/example/project/releases/download/v1/asset"}, SHA256: strings.TrimPrefix(digestOf(body), "sha256:"), Size: int64(len(body))}
}

func TestArtifactResolverDoesNotDownloadAndPinsExactRelease(t *testing.T) {
	queries := 0
	f := Fetcher{Query: func(ctx context.Context, repo, tag string, count int) ([]ghrelease.Release, error) {
		queries++
		if repo != "XTLS/Xray-core" || tag != "v26.3.27" || count != 0 {
			t.Fatalf("unexpected metadata query: %s %s %d", repo, tag, count)
		}
		return []ghrelease.Release{{Tag: tag, Assets: []ghrelease.Asset{{Name: "Xray-linux-64.zip", URL: "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip", Digest: "sha256:" + strings.Repeat("a", 64), Size: 123}}}}, nil
	}}
	artifact, err := f.ResolveArtifact(t.Context(), "xray", "26.3.27", "amd64")
	if err != nil || queries != 1 || artifact.SHA256 != strings.Repeat("a", 64) || artifact.Size != 123 {
		t.Fatalf("artifact: %+v %v", artifact, err)
	}
}

func TestExtractArtifactChecksDigestArchitectureAndRegularMember(t *testing.T) {
	amd64, arm64 := artifactELF(62), artifactELF(183)
	for _, item := range []struct {
		kernel, version, arch string
		archive, want         []byte
	}{
		{"xray", "26.3.27", "amd64", zipWith("xray", amd64), amd64},
		{"sing-box", "1.14.3", "arm64", tarGzWith("sing-box-1.14.3-linux-arm64/sing-box", arm64), arm64},
		{"mihomo", "1.19.33", "amd64", gzWith(amd64), amd64},
	} {
		artifact := artifactFor(item.kernel, item.version, item.arch, item.archive)
		got, err := ExtractArtifact(artifact, item.archive)
		if err != nil || !bytes.Equal(got, item.want) {
			t.Fatalf("%s extraction: %v", item.kernel, err)
		}
		artifact.SHA256 = strings.Repeat("a", 64)
		if _, err := ExtractArtifact(artifact, item.archive); err == nil {
			t.Fatal("tampered archive accepted")
		}
	}
	wrong := zipWith("xray", arm64)
	if _, err := ExtractArtifact(artifactFor("xray", "26.3.27", "amd64", wrong), wrong); err == nil {
		t.Fatal("wrong architecture accepted")
	}
	for _, name := range []string{"../xray", "nested/xray"} {
		archive := zipWith(name, amd64)
		if _, err := ExtractArtifact(artifactFor("xray", "26.3.27", "amd64", archive), archive); err == nil {
			t.Fatal("unexpected archive member accepted", name)
		}
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "xray"}
	h.SetMode(os.ModeSymlink | 0777)
	member, _ := w.CreateHeader(h)
	_, _ = member.Write(amd64)
	_ = w.Close()
	if _, err := ExtractArtifact(artifactFor("xray", "26.3.27", "amd64", buf.Bytes()), buf.Bytes()); err == nil {
		t.Fatal("symlink member accepted")
	}
}
