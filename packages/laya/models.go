package laya

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIError contains protocol metadata only, never remote text or credentials.
type APIError struct {
	Code       string
	HTTPStatus int
}

func (e *APIError) Error() string { return e.Code }

type ModelProgress struct {
	Stage            string
	Digest           string
	Completed, Total int64
}

type ModelStatus struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// RuntimeVersion reads the runtime's own metadata, separately from a host's
// approved image label. Missing or malformed metadata is never a version match.
func (c Client) RuntimeVersion(ctx context.Context) (version string, err error) {
	started := time.Now().UTC()
	defer func() { emit(c.Observer, ctx, "laya.runtime.version", started, err) }()
	endpoint, err := c.endpoint("/api/version")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", &APIError{Code: "invalid_request"}
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.HTTP
	if client == nil {
		client = runtimeHTTP(5 * time.Second)
	}
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &APIError{Code: "runtime_version_unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", &APIError{Code: "runtime_version_unavailable", HTTPStatus: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	var info struct {
		Version string `json:"version"`
	}
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &info) != nil {
		return "", &APIError{Code: "runtime_version_invalid"}
	}
	if _, ok := parseVersion(info.Version); !ok || len(info.Version) > 100 {
		return "", &APIError{Code: "runtime_version_invalid"}
	}
	return info.Version, nil
}

var defaultRuntimeTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return transport
}()

func runtimeHTTP(timeout time.Duration) *http.Client {
	return &http.Client{Transport: defaultRuntimeTransport, Timeout: timeout}
}

func (c Client) endpoint(path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", &APIError{Code: "invalid_endpoint"}
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !(u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
			return "", &APIError{Code: "endpoint_requires_https"}
		}
	}
	base = strings.TrimSuffix(base, "/v1/systemone")
	base = strings.TrimSuffix(base, "/v1")
	return base + path, nil
}

func (c Client) api(ctx context.Context, path string, body any) (*http.Response, error) {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, &APIError{Code: "invalid_request"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, &APIError{Code: "invalid_request"}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.HTTP
	if client == nil {
		client = runtimeHTTP(30 * time.Minute)
	}
	// Redirects must never forward a runtime token or turn a loopback request
	// into a call to another host, even with a caller-supplied transport.
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &APIError{Code: "connection_failed"}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&failure)
		return nil, &APIError{Code: modelErrorCode(failure.Code), HTTPStatus: resp.StatusCode}
	}
	return resp, nil
}

func modelErrorCode(code string) string {
	switch code {
	case "DIGEST_MISMATCH":
		return "model_digest_mismatch"
	case "MODEL_NOT_FOUND":
		return "model_not_found"
	case "OPERATION_IN_PROGRESS":
		return "model_operation_in_progress"
	case "STORAGE_ERROR":
		return "model_storage_failed"
	case "REGISTRY_ERROR":
		return "model_registry_unavailable"
	default:
		return "model_api_failed"
	}
}

// PrepareModel waits for the official listener and installs a missing pinned
// model. It does not infer health from metadata or execute a decision request.
func (c Client) PrepareModel(ctx context.Context, model Model, progress func(ModelProgress)) error {
	if len(model.Manifest) == 0 || validateModelManifest(model) != nil || model.Name == "" || strings.ContainsAny(model.Name, "@?#\\ \t\r\n") {
		return &APIError{Code: "model_manifest_invalid"}
	}
	digest := sha256.Sum256(model.Manifest)
	name := model.Name + "@sha256:" + hex.EncodeToString(digest[:])
	ready, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		status, err := c.ShowModel(ready, name)
		if err == nil {
			if status.Digest != hex.EncodeToString(digest[:]) {
				return &APIError{Code: "model_digest_mismatch"}
			}
			return nil
		}
		var detail *APIError
		if errors.As(err, &detail) {
			if detail.HTTPStatus == 404 || detail.HTTPStatus == 409 {
				break
			}
			if detail.Code != "connection_failed" {
				return err
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ready.Err() != nil {
			return &APIError{Code: "runtime_readiness_timeout"}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ready.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &APIError{Code: "runtime_readiness_timeout"}
		case <-timer.C:
		}
	}
	return c.PullModel(ctx, model, progress)
}

// ShowModel resolves a name, optionally pinned by manifest digest. This reports
// installation metadata; successful inference is a separate health check.
func (c Client) ShowModel(ctx context.Context, name string) (out ModelStatus, err error) {
	started := time.Now().UTC()
	defer func() { emit(c.Observer, ctx, "laya.model.show", started, err) }()
	resp, err := c.api(ctx, "/api/show", struct {
		Model string `json:"model"`
	}{name})
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return out, &APIError{Code: "model_response_invalid"}
	}
	var info struct {
		Digest string `json:"digest"`
	}
	if json.Unmarshal(raw, &info) != nil {
		return out, &APIError{Code: "model_response_invalid"}
	}
	digest, err := hex.DecodeString(info.Digest)
	if err != nil || len(digest) != sha256.Size || info.Digest != strings.ToLower(info.Digest) {
		return out, &APIError{Code: "model_response_invalid"}
	}
	return ModelStatus{Name: name, Digest: info.Digest}, nil
}

// PullModel delegates downloads and resumption to the official runtime. The
// request pins the exact manifest bytes, and ollaya verifies every downloaded
// blob before atomically publishing the manifest. No executable is compiled.
func (c Client) PullModel(ctx context.Context, model Model, progress func(ModelProgress)) (err error) {
	started := time.Now().UTC()
	defer func() { emit(c.Observer, ctx, "laya.model.pull", started, err) }()
	if model.Name == "" || strings.ContainsAny(model.Name, "@?#\\ \t\r\n") || len(model.Manifest) == 0 || validateModelManifest(model) != nil {
		return &APIError{Code: "model_manifest_invalid"}
	}
	sum := sha256.Sum256(model.Manifest)
	digest := hex.EncodeToString(sum[:])
	name := model.Name + "@sha256:" + digest
	resp, err := c.api(ctx, "/api/pull", struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}{name, true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	files := map[string]int64{}
	for _, file := range model.Files {
		files["sha256:"+file.SHA256] = file.Size
	}
	limited := &io.LimitedReader{R: resp.Body, N: (16 << 20) + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	complete := false
	for scanner.Scan() {
		if complete || limited.N <= 0 {
			return &APIError{Code: "model_progress_invalid"}
		}
		var line struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Completed int64  `json:"completed"`
			Total     int64  `json:"total"`
			Error     string `json:"error"`
			Code      string `json:"code"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			return &APIError{Code: "model_progress_invalid"}
		}
		if line.Error != "" || line.Code != "" {
			return &APIError{Code: modelErrorCode(line.Code)}
		}
		p := ModelProgress{Stage: "preparing"}
		switch line.Status {
		case "success":
			p.Stage, complete = "complete", true
		case "verifying sha256 digest":
			p.Stage = "verifying"
		case "writing manifest":
			p.Stage = "activating"
		}
		if line.Digest != "" {
			if size, ok := files[line.Digest]; !ok || size != line.Total || line.Completed < 0 || line.Completed > size {
				return &APIError{Code: "model_progress_invalid"}
			}
			p = ModelProgress{Stage: "downloading", Digest: line.Digest, Completed: line.Completed, Total: line.Total}
		}
		if progress != nil && !complete {
			progress(p)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanner.Err() != nil || !complete {
		return &APIError{Code: "model_pull_interrupted"}
	}
	status, err := c.ShowModel(ctx, name)
	if err != nil {
		return err
	}
	if status.Digest != digest {
		return &APIError{Code: "model_digest_mismatch"}
	}
	if progress != nil {
		progress(ModelProgress{Stage: "complete"})
	}
	return nil
}
