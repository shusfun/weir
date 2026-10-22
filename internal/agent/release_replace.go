package agent

import (
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
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/packages/contracts"
)

const releaseReplaceOperation = "release.replace"

// 单个 migate 程序的上限；当前约 40 MiB，留出增长空间。
const maxReleaseBinary = 128 << 20

var releaseSum = regexp.MustCompile(`^[0-9a-f]{64}$`)

// replaceRelease 任务只携带版本与摘要。Agent 凭设备身份从中心下载程序、校验摘要，
// 再以原始字节交给 root 辅助程序安装；任务内容里不再嵌入程序本体。
func (a *Agent) replaceRelease(ctx context.Context, t contracts.Task) (map[string]any, error) {
	version, sum, err := DecodeReleasePayload(t.Payload)
	if err != nil {
		return nil, err
	}
	binary, err := a.downloadRelease(ctx, version)
	if err != nil {
		return nil, err
	}
	if err = matchSum(binary, sum); err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/release-replace", bytes.NewReader(binary))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Release-Version", version)
	req.Header.Set("X-Release-SHA256", sum)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("release_helper_unavailable")
	}
	defer resp.Body.Close()
	var result map[string]any
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&result) != nil {
		return nil, errors.New("release_replace_invalid_response")
	}
	if resp.StatusCode != 200 {
		message := fmtText(result["message"])
		if message == "" {
			message = "release_replace_failed"
		}
		return nil, errors.New(message)
	}
	return result, nil
}

// downloadRelease 只访问中心固定路径，版本号来自已校验的任务；不接受任意 URL。
func (a *Agent) downloadRelease(ctx context.Context, version string) ([]byte, error) {
	if err := a.authorize(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", a.Config.Center+"/api/v1/agent/release/"+url.PathEscape(version), nil)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	req.Header.Set("Authorization", "Bearer "+a.token)
	a.mu.Unlock()
	client := http.Client{Transport: a.HTTP.Transport, Timeout: 10 * time.Minute, CheckRedirect: a.HTTP.CheckRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("release_download_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("release_download_failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseBinary+1))
	if err != nil || len(data) == 0 || len(data) > maxReleaseBinary {
		return nil, errors.New("release_body_refused")
	}
	return data, nil
}

// InstallRelease 把程序写入 releases/<版本>/migate，并让 migate-gate / migate-manage
// 指向它（按调用名分发），随后切换 current 并保留 previous。同版本重放不重复安装。
func InstallRelease(root, version string, binary []byte, sum string, restart func() error) error {
	parsed, ok := buildinfo.ParseScheme(version)
	if !ok || parsed.String() != version {
		return errors.New("invalid_release_version")
	}
	if len(binary) == 0 || len(binary) > maxReleaseBinary {
		return errors.New("release_body_refused")
	}
	if err := matchSum(binary, sum); err != nil {
		return err
	}
	releaseDir := filepath.Join(root, "releases", version)
	if !within(root, releaseDir) || filepath.Base(releaseDir) == "cores" {
		return errors.New("release_path_refused")
	}
	current := filepath.Join(root, "current")
	if sameRelease(current, releaseDir, filepath.Join(releaseDir, "migate"), binary) {
		return nil
	}
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		return err
	}
	if err := writeExecutable(filepath.Join(releaseDir, "migate"), binary); err != nil {
		return err
	}
	for _, name := range []string{"migate-gate", "migate-manage"} {
		link := filepath.Join(releaseDir, name)
		_ = os.Remove(link)
		if err := os.Symlink("migate", link); err != nil {
			return err
		}
	}
	previous := filepath.Join(root, "previous")
	if link, err := os.Readlink(current); err == nil {
		_ = os.Remove(previous)
		if err = os.Symlink(link, previous); err != nil {
			return err
		}
	}
	_ = os.Remove(current)
	if err := os.Symlink(filepath.Join("releases", version), current); err != nil {
		return err
	}
	if restart != nil {
		if err := restart(); err != nil {
			return errors.New("release_restart_failed")
		}
	}
	return nil
}

// DecodeReleasePayload 只接受版本与摘要两个字段；URL、令牌或程序本体一律拒绝。
func DecodeReleasePayload(payload map[string]any) (version, sum string, err error) {
	for key := range payload {
		switch key {
		case "version", "sha256":
		default:
			return "", "", errors.New("release_payload_refused")
		}
	}
	version = fmtText(payload["version"])
	sum = fmtText(payload["sha256"])
	if parsed, ok := buildinfo.ParseScheme(version); !ok || parsed.String() != version {
		return "", "", errors.New("invalid_release_version")
	}
	if !releaseSum.MatchString(sum) {
		return "", "", errors.New("release_payload_refused")
	}
	return version, sum, nil
}

func matchSum(data []byte, sum string) error {
	digest := sha256.Sum256(data)
	if !bytes.Equal([]byte(sum), []byte(hex.EncodeToString(digest[:]))) {
		return errors.New("release_checksum_mismatch")
	}
	return nil
}

func writeExecutable(path string, data []byte) error {
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, 0755); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func sameRelease(current, releaseDir, binaryPath string, binary []byte) bool {
	link, err := os.Readlink(current)
	if err != nil || filepath.Clean(link) != filepath.Clean(filepath.Join("releases", filepath.Base(releaseDir))) {
		return false
	}
	existing, err := os.ReadFile(binaryPath)
	return err == nil && bytes.Equal(existing, binary)
}

// releaseReplaceHandler 由 root 辅助程序提供：请求体为程序原始字节，版本与摘要在请求头。
func releaseReplaceHandler(root string, restart func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		version, sum, err := DecodeReleasePayload(map[string]any{"version": r.Header.Get("X-Release-Version"), "sha256": r.Header.Get("X-Release-SHA256")})
		if err != nil {
			writeReleaseError(w, err)
			return
		}
		binary, err := io.ReadAll(io.LimitReader(r.Body, maxReleaseBinary+1))
		if err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		if err = InstallRelease(root, version, binary, sum, restart); err != nil {
			writeReleaseError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"applied":true}`))
	}
}

func writeReleaseError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(422)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
}
