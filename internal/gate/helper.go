package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imzyb/MiGate/packages/contracts"
)

var helperKernels = []string{"xray", "sing-box"}
var safeVer = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

// ApplyRequest 是 Agent 交给辅助程序的一次整体应用：每个内核的版本与配置（nil 表示停止该内核）。
type ApplyRequest struct {
	Kernels    map[string]string `json:"kernels"`
	Configs    map[string][]byte `json:"configs"`
	StatsPorts map[string]int    `json:"stats_ports"`
	Ports      []int             `json:"ports"` // 本次配置需要监听的全部端口，用于冲突检查
}

type ApplyResult struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	ConflictPort int    `json:"conflict_port,omitempty"`
}

type HelperStatus struct {
	Installed map[string][]string       `json:"installed"`
	Active    map[string]string         `json:"active"`
	Running   map[string]int            `json:"running"`
	Listening []contracts.ListeningPort `json:"listening"`
}

// Helper 是以 root 运行的辅助程序。只提供白名单接口，不执行任意命令或任意路径。
type Helper struct {
	P        Paths
	ProcRoot string
	Sup      *Supervisor
	mu       sync.Mutex
}

func NewHelper(p Paths) *Helper { return &Helper{P: p, ProcRoot: "/proc", Sup: NewSupervisor(p)} }

func (h *Helper) pointer(name string) string {
	b, _ := os.ReadFile(filepath.Join(h.P.KernelsDir, name+".version"))
	return strings.TrimSpace(string(b))
}

func (h *Helper) setPointer(name, version string) error {
	path := filepath.Join(h.P.KernelsDir, name+".version")
	if version == "" {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(version), 0644)
}

// Boot 在辅助程序启动时按磁盘上已生效的配置拉起内核（Agent 未连上中心也能继续服务）。
func (h *Helper) Boot() {
	for _, name := range helperKernels {
		if _, err := os.Stat(h.Sup.config(name)); err == nil && h.pointer(name) != "" {
			_ = h.Sup.Start(name)
		}
	}
}

func (h *Helper) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /kernel", h.installKernel)
	mux.HandleFunc("POST /apply", h.apply)
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("POST /restart", h.restart)
	mux.HandleFunc("POST /cert", h.cert)
	mux.HandleFunc("GET /diagnostics", h.diagnostics)
	mux.HandleFunc("POST /release", h.release)
	mux.HandleFunc("POST /uninstall", h.uninstall)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func known(name string) bool { return name == "xray" || name == "sing-box" }

func sumOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func (h *Helper) installKernel(w http.ResponseWriter, r *http.Request) {
	name, version, want := r.Header.Get("X-Kernel"), r.Header.Get("X-Version"), r.Header.Get("X-SHA256")
	if !known(name) || !safeVer.MatchString(version) || len(want) != 64 {
		writeJSON(w, 400, ApplyResult{Error: "invalid kernel reference"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
	if err != nil || sumOf(data) != want {
		writeJSON(w, 422, ApplyResult{Error: "kernel checksum mismatch"})
		return
	}
	if err = os.MkdirAll(h.P.KernelsDir, 0755); err == nil {
		dst := filepath.Join(h.P.KernelsDir, name+"-"+version)
		if err = os.WriteFile(dst+".new", data, 0755); err == nil {
			err = os.Rename(dst+".new", dst)
		}
	}
	if err != nil {
		writeJSON(w, 500, ApplyResult{Error: err.Error()})
		return
	}
	writeJSON(w, 200, ApplyResult{OK: true})
}

func (h *Helper) installed() map[string][]string {
	out := map[string][]string{}
	entries, _ := os.ReadDir(h.P.KernelsDir)
	for _, e := range entries {
		for _, name := range helperKernels {
			if v := strings.TrimPrefix(e.Name(), name+"-"); v != e.Name() && !strings.HasSuffix(v, ".new") && safeVer.MatchString(v) {
				out[name] = append(out[name], v)
			}
		}
	}
	return out
}

func (h *Helper) status(w http.ResponseWriter, _ *http.Request) {
	ports, _ := listening(h.ProcRoot)
	active := map[string]string{}
	for _, name := range helperKernels {
		if v := h.pointer(name); v != "" {
			active[name] = v
		}
	}
	writeJSON(w, 200, HelperStatus{Installed: h.installed(), Active: active, Running: h.Sup.Running(), Listening: ports})
}

// healthy 等待内核进程存活且统计端口可连接（最多 8 秒）。
func (h *Helper) healthy(name string, port int) bool {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := h.Sup.Running()[name]; ok && port > 0 {
			if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond); err == nil {
				c.Close()
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (h *Helper) apply(w http.ResponseWriter, r *http.Request) {
	var req ApplyRequest
	if json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&req) != nil {
		writeJSON(w, 400, ApplyResult{Error: "invalid apply request"})
		return
	}
	res := h.Apply(r.Context(), req)
	code := 200
	if !res.OK {
		code = 422
	}
	writeJSON(w, code, res)
}

// Apply 逐个内核：冲突检查 → 自检（候选文件 + 目标版本）→ 切换 → 健康检查；失败回滚到上一份配置与版本。
func (h *Helper) Apply(ctx context.Context, req ApplyRequest) ApplyResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, pids := listening(h.ProcRoot)
	ours := map[int]bool{}
	for _, pid := range h.Sup.Running() {
		ours[pid] = true
	}
	if port := conflicts(req.Ports, pids, ours); port != 0 {
		return ApplyResult{Error: fmt.Sprintf("端口 %d 已被其他程序占用", port), ConflictPort: port}
	}
	if err := os.MkdirAll(h.P.CoresDir, 0750); err != nil {
		return ApplyResult{Error: err.Error()}
	}
	for _, name := range helperKernels {
		cur := h.Sup.config(name)
		body, want := req.Configs[name], req.Kernels[name]
		if body == nil {
			if _, err := os.Stat(cur); err == nil {
				h.Sup.Stop(name)
				_ = os.Rename(cur, cur+".prev")
			}
			continue
		}
		if !safeVer.MatchString(want) {
			return ApplyResult{Error: name + " version missing"}
		}
		old := h.pointer(name)
		if existing, err := os.ReadFile(cur); err == nil && old == want && bytes.Equal(existing, body) {
			if _, running := h.Sup.Running()[name]; running {
				continue
			}
		}
		// 内核按扩展名识别配置格式，候选文件必须仍以 .json 结尾。
		cand := strings.TrimSuffix(cur, ".json") + ".candidate.json"
		if err := os.WriteFile(cand, body, 0640); err != nil {
			return ApplyResult{Error: err.Error()}
		}
		h.shareWithAgent(cand)
		if err := h.Sup.Test(ctx, name, want, cand); err != nil {
			_ = os.Remove(cand)
			return ApplyResult{Error: err.Error()}
		}
		_, hadPrev := os.Stat(cur)
		if hadPrev == nil {
			_ = os.Rename(cur, cur+".prev")
		}
		if err := os.Rename(cand, cur); err != nil {
			return ApplyResult{Error: err.Error()}
		}
		_ = h.setPointer(name, want)
		if err := h.Sup.Start(name); err != nil || !h.healthy(name, req.StatsPorts[name]) {
			h.Sup.Stop(name)
			if hadPrev == nil {
				_ = os.Rename(cur+".prev", cur)
				_ = h.setPointer(name, old)
				_ = h.Sup.Start(name)
			} else {
				_ = os.Remove(cur)
				_ = h.setPointer(name, old)
			}
			msg := name + " 启动后健康检查未通过，已回滚"
			if logs := h.Sup.Logs(name); len(logs) > 0 {
				msg += "：" + logs[len(logs)-1]
			}
			return ApplyResult{Error: msg}
		}
	}
	return ApplyResult{OK: true}
}

// shareWithAgent 把文件属组设为内核运行账号，使降权后的内核能读取配置与证书。
func (h *Helper) shareWithAgent(path string) {
	if os.Geteuid() != 0 || h.P.RunAs == "" {
		return
	}
	if g, err := user.LookupGroup(h.P.RunAs); err == nil {
		gid, _ := strconv.Atoi(g.Gid)
		_ = os.Chown(path, 0, gid)
	}
}

func (h *Helper) restart(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !known(name) {
		writeJSON(w, 400, ApplyResult{Error: "unknown kernel"})
		return
	}
	if err := h.Sup.Start(name); err != nil {
		writeJSON(w, 422, ApplyResult{Error: err.Error()})
		return
	}
	writeJSON(w, 200, ApplyResult{OK: true})
}

func (h *Helper) cert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	var body struct {
		Cert string `json:"cert_pem"`
		Key  string `json:"key_pem"`
	}
	if err != nil || id <= 0 || json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body) != nil || !strings.Contains(body.Cert, "BEGIN CERTIFICATE") || !strings.Contains(body.Key, "PRIVATE KEY") {
		writeJSON(w, 400, ApplyResult{Error: "invalid certificate"})
		return
	}
	if err = os.MkdirAll(h.P.CertsDir, 0750); err != nil {
		writeJSON(w, 500, ApplyResult{Error: err.Error()})
		return
	}
	for ext, data := range map[string]string{"crt": body.Cert, "key": body.Key} {
		path := filepath.Join(h.P.CertsDir, fmt.Sprintf("%d.%s", id, ext))
		if err = os.WriteFile(path+".new", []byte(data), 0640); err == nil {
			h.shareWithAgent(path + ".new")
			err = os.Rename(path+".new", path)
		}
		if err != nil {
			writeJSON(w, 500, ApplyResult{Error: err.Error()})
			return
		}
	}
	writeJSON(w, 200, ApplyResult{OK: true})
}

var ipv4 = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
var ipv6 = regexp.MustCompile(`\b[0-9a-fA-F]{1,4}(:[0-9a-fA-F]{0,4}){2,7}\b`)

// diagnostics 只返回本地分类计数与最近 50 行（IP 地址已遮盖），不回传完整原始日志。
func (h *Helper) diagnostics(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !known(name) {
		writeJSON(w, 400, ApplyResult{Error: "unknown kernel"})
		return
	}
	logs := h.Sup.Logs(name)
	counts := map[string]int{}
	for _, line := range logs {
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "error") || strings.Contains(lower, "fatal"):
			counts["error"]++
		case strings.Contains(lower, "warn"):
			counts["warning"]++
		default:
			counts["info"]++
		}
	}
	if len(logs) > 50 {
		logs = logs[len(logs)-50:]
	}
	masked := make([]string, 0, len(logs))
	for _, line := range logs {
		masked = append(masked, ipv6.ReplaceAllString(ipv4.ReplaceAllString(line, "x.x.x.x"), "x:x::x"))
	}
	_, running := h.Sup.Running()[name]
	writeJSON(w, 200, map[string]any{"running": running, "version": h.pointer(name), "counts": counts, "tail": masked, "truncated": len(h.Sup.Logs(name)) > 50})
}

// release 安装新版本程序到 releases/<版本>，切换 current 并保留 previous，然后重启 Gate 服务。
func (h *Helper) release(w http.ResponseWriter, r *http.Request) {
	version, want := r.Header.Get("X-Version"), r.Header.Get("X-SHA256")
	if !safeVer.MatchString(version) || len(want) != 64 {
		writeJSON(w, 400, ApplyResult{Error: "invalid release"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
	if err != nil || sumOf(data) != want {
		writeJSON(w, 422, ApplyResult{Error: "release checksum mismatch"})
		return
	}
	dir := filepath.Join(h.P.Root, "releases", version)
	if err = os.MkdirAll(dir, 0755); err == nil {
		if err = os.WriteFile(filepath.Join(dir, "migate.new"), data, 0755); err == nil {
			err = os.Rename(filepath.Join(dir, "migate.new"), filepath.Join(dir, "migate"))
		}
	}
	if err == nil {
		current := filepath.Join(h.P.Root, "current")
		if link, e := os.Readlink(current); e == nil && filepath.Base(link) != version {
			_ = os.Remove(filepath.Join(h.P.Root, "previous"))
			_ = os.Symlink(link, filepath.Join(h.P.Root, "previous"))
		}
		_ = os.Remove(current + ".new")
		if err = os.Symlink(filepath.Join("releases", version), current+".new"); err == nil {
			err = os.Rename(current+".new", current)
		}
	}
	if err != nil {
		writeJSON(w, 500, ApplyResult{Error: err.Error()})
		return
	}
	writeJSON(w, 200, ApplyResult{OK: true})
	go func() {
		time.Sleep(time.Second)
		_ = exec.Command("systemctl", "restart", "--no-block", "migate-helper.service", "migate-gate.service").Run()
	}()
}

// uninstall 只在本机角色为 Gate 时执行，调用安装器的卸载（保留配置与数据）。
func (h *Helper) uninstall(w http.ResponseWriter, _ *http.Request) {
	role, _ := os.ReadFile("/etc/migate/role")
	if strings.TrimSpace(string(role)) != "gate" {
		writeJSON(w, 409, ApplyResult{Error: "only a gate-only machine can be uninstalled remotely"})
		return
	}
	writeJSON(w, 200, ApplyResult{OK: true})
	go func() {
		time.Sleep(time.Second)
		_ = exec.Command("sh", "/usr/local/lib/migate/install.sh", "uninstall").Run()
	}()
}

// ServeHelper 在本机套接字上提供辅助接口：属组 migate-gate、权限 0660，只有 Agent 能访问。
func ServeHelper(ctx context.Context, h *Helper) error {
	if err := os.MkdirAll(filepath.Dir(h.P.Socket), 0750); err != nil {
		return err
	}
	_ = os.Remove(h.P.Socket)
	ln, err := net.Listen("unix", h.P.Socket)
	if err != nil {
		return err
	}
	_ = os.Chmod(h.P.Socket, 0660)
	h.shareWithAgent(h.P.Socket)
	h.Boot()
	srv := &http.Server{Handler: h.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		for _, name := range helperKernels {
			h.Sup.Stop(name)
		}
		_ = srv.Close()
	}()
	if err = srv.Serve(ln); errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
