package contracts

import (
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const KernelArtifactVersion = 1
const MaxKernelArchiveBytes int64 = 200 << 20

var kernelVersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

// Archive members and formats are selected by the local kernel adapter;
// a remote artifact cannot supply executable extraction instructions.
type KernelArtifact struct {
	Schema  int      `json:"schema"`
	Kernel  string   `json:"kernel"`
	Version string   `json:"version"`
	Arch    string   `json:"arch"`
	URLs    []string `json:"urls"`
	SHA256  string   `json:"sha256"`
	Size    int64    `json:"size"`
}

type KernelInstallResult struct {
	Kernel        string `json:"kernel"`
	Version       string `json:"version"`
	Arch          string `json:"arch"`
	ArchiveSHA256 string `json:"archive_sha256"`
	BinarySHA256  string `json:"binary_sha256"`
}

func (a KernelArtifact) Validate(kernel, version, arch string) error {
	sum, err := hex.DecodeString(a.SHA256)
	if a.Schema != KernelArtifactVersion || a.Kernel != kernel || a.Version != version || a.Arch != arch || kernel != "xray" && kernel != "sing-box" && kernel != "mihomo" || arch != "amd64" && arch != "arm64" || !kernelVersionPattern.MatchString(version) || err != nil || len(sum) != 32 || strings.ToLower(a.SHA256) != a.SHA256 || a.Size < 1 || a.Size > MaxKernelArchiveBytes || len(a.URLs) < 1 || len(a.URLs) > 20 {
		return errors.New("kernel_artifact_invalid")
	}
	for _, raw := range a.URLs {
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 4096 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return errors.New("kernel_artifact_source_invalid")
		}
	}
	return nil
}
