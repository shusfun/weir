package contracts

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var taskIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var taskProbeHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)

func taskDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && value == strings.ToLower(value)
}

func taskURL(raw string) bool {
	return taskURLBound(raw, 16<<10)
}

func taskURLBound(raw string, limit int) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= limit && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(raw, "\r\n\x00")
}

func taskHeader(raw string, limit int) bool {
	return len(raw) <= limit && !strings.ContainsAny(raw, "\r\n\x00")
}

func taskNetworkInput(secret FetchSecrets, limit int64) bool {
	return taskURL(secret.URL) && taskHeader(secret.Authorization, 8192) && limit > 0 && limit <= 10<<20
}

func taskKernelRefs(refs map[string]Kernel) bool {
	if len(refs) == 0 || len(refs) > 3 {
		return false
	}
	for name, ref := range refs {
		if name != "xray" && name != "sing-box" && name != "mihomo" || !kernelVersionPattern.MatchString(ref.Version) {
			return false
		}
		if ref.Artifact != nil {
			if ref.Artifact.Validate(name, ref.Version, ref.Artifact.Arch) != nil || ref.SHA256 != "" && !taskDigest(ref.SHA256) {
				return false
			}
		} else if !taskDigest(ref.SHA256) {
			return false
		}
	}
	return true
}

func validateTaskRequest(value any) error {
	valid := true
	switch req := value.(type) {
	case *NodeProbeTask:
		valid = taskURL(req.Credentials.URL) && taskHeader(req.Credentials.Authorization, 256) && len(req.Client.Proxies) == 1 && req.Client.Target != "" && taskKernelRefs(req.Kernels)
	case *SubscriptionProbeTask:
		valid = taskURL(req.Target) && len(req.Proxies) > 0 && len(req.Proxies) <= 200 && taskKernelRefs(req.Kernels)
	case *EmptyTask:
	case *ReleaseQueryTask:
		return req.Validate()
	case *KernelInstallTask:
		return req.Artifact.Validate(req.Kernel, req.Version, req.Arch)
	case *AssetFetch:
		valid = taskIdentifier.MatchString(req.UploadID) && taskDigest(req.SHA256) && req.MaxSize >= 0 && req.MaxSize <= 512<<20 && req.Size >= 0 && req.Size <= 512<<20 && (req.MaxSize == 0 || req.Size <= req.MaxSize) && len(req.URLs) > 0 && len(req.URLs) <= 20
		for _, raw := range req.URLs {
			valid = valid && taskURL(raw)
		}
	case *SourceProbe:
		valid = taskURL(req.URL) && req.Bytes >= 0 && req.Bytes <= 4<<20
	case *KernelTask:
		valid = req.Kernel == "xray" || req.Kernel == "sing-box"
	case *RealityProbeTask:
		valid = len(req.Targets) > 0 && len(req.Targets) <= 20
		for _, host := range req.Targets {
			valid = valid && taskProbeHost.MatchString(host) && strings.Contains(host, ".")
		}
	case *LiveTask:
		valid = req.Seconds >= 1 && req.Seconds <= 300
	case *PanelConfigureTask:
		valid = taskIdentifier.MatchString(req.Nonce) && req.Port > 0 && req.Port <= 65535
	case *PanelCertificateTask:
		valid = req.CertificateID >= 0 && net.ParseIP(req.IP) != nil
	case *ACMETask:
		valid = taskIdentifier.MatchString(req.Token) && taskHeader(req.KeyAuthorization, 512) && strings.HasPrefix(req.KeyAuthorization, req.Token+".") && req.Seconds >= 1 && req.Seconds <= 300 && (net.ParseIP(req.Host) != nil || taskProbeHost.MatchString(req.Host))
	case *ReleaseTarget:
		valid = kernelVersionPattern.MatchString(req.Version) && (len(req.URLs) == 0 && req.SHA256 == "" || taskDigest(req.SHA256)) && len(req.URLs) <= 20
		for _, raw := range req.URLs {
			valid = valid && taskURL(raw)
		}
	case *NodeTestTask:
		valid = len(req.Config.Inbounds) == 1 && taskKernelRefs(req.Config.Kernels) && req.Client.Target != "" && len(req.Client.Target) <= 256 && len(req.Client.Proxies) == 1
	case *OutboundProbeTask:
		valid = req.Address != "" && len(req.Address) <= 253 && !strings.ContainsAny(req.Address, "/\\\x00\r\n") && req.Port > 0 && req.Port <= 65535 && taskURL(req.Target) && taskKernelRefs(req.Kernels) && len(req.Client) > 0 && !bytes.Equal(bytes.TrimSpace(req.Client), []byte("null"))
	case *SubscriptionFetchTask:
		valid = taskNetworkInput(req.Credentials, req.MaxBytes) && taskHeader(req.UserAgent, 1024)
	case *ExternalConvertTask:
		valid = taskURLBound(req.Credentials.URL, 64<<10) && taskHeader(req.Credentials.Authorization, 8192) && req.MaxBytes > 0 && req.MaxBytes <= 10<<20 && len(req.Content) <= 768<<10 && taskHeader(req.ContentType, 256) && (req.Method == "" || req.Method == "POST" || req.Method == "GET" && len(req.Content) == 0)
	case *TraceReadTask:
		trace, err := hex.DecodeString(req.TraceID)
		valid = err == nil && len(trace) == 16 && req.TraceID != strings.Repeat("0", 32) && req.After >= 0 && req.Limit >= 1 && req.Limit <= 200
	case *LayaInstallTask:
		var manifest struct {
			ImageDigest string `json:"image_digest"`
		}
		valid = req.DeploymentVersion != "" && len(req.DeploymentVersion) <= 100 && DecodeStrict(req.Manifest, &manifest, 4096) == nil && strings.HasPrefix(manifest.ImageDigest, "sha256:") && taskDigest(strings.TrimPrefix(manifest.ImageDigest, "sha256:"))
	case *LayaClassifyTask:
		valid = req.Model != "" && len(req.Model) <= 200 && len(req.ExpectedRuntimeVersion) <= 100 && len(req.State) > 0 && !bytes.Equal(bytes.TrimSpace(req.State), []byte("null")) && len(req.Questions) > 0 && len(req.Questions) <= 16
		for name, q := range req.Questions {
			valid = valid && taskIdentifier.MatchString(name) && (q.Type == "choice" || q.Type == "score" || q.Type == "noul") && len(q.Instructions) <= 32<<10 && len(q.Criteria) <= 32<<10
		}
	default:
		valid = false
	}
	if !valid {
		return errors.New("task_payload_invalid")
	}
	return nil
}
