package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shusfun/weir/packages/contracts"
)

func firstMode(current, next string) string {
	if current == "" || current == contracts.ApplyUnchanged {
		return next
	}
	if next == contracts.ApplyRestart {
		return next
	}
	return current
}

func errString(err error) string {
	if err == nil {
		return "健康检查未通过"
	}
	return err.Error()
}

func writeAtomic(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (h *Helper) applyUserDelta(ctx context.Context, kernel string, port int, added, removed []userChange) error {
	if port <= 0 {
		return fmt.Errorf("%s 没有统计端口，不能热更新用户", kernel)
	}
	server := fmt.Sprintf("127.0.0.1:%d", port)
	bin, err := h.kernelBinary(kernel)
	if err != nil {
		return err
	}
	for _, item := range removed {
		if err := h.xrayRemoveUser(ctx, bin, server, item.Tag, item.Email); err != nil {
			return err
		}
	}
	for _, item := range added {
		if err := h.xrayAddUser(ctx, bin, server, item); err != nil {
			return err
		}
	}
	return nil
}

func (h *Helper) kernelBinary(name string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(h.P.KernelsDir, name+".version"))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(raw))
	if version == "" {
		return "", fmt.Errorf("%s version missing", name)
	}
	return filepath.Join(h.P.KernelsDir, name+"-"+version), nil
}

func runXrayAPI(ctx context.Context, bin string, args []string, stdin []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("%s", text)
	}
	return text, nil
}

// xrayAddUser 按 Xray 26.3 的 `api adu` 写入一份入站配置。
// 命令把 JSON 当配置文件解析，再调用 HandlerService.AlterInbound/AddUserOperation。
// 接口失败时进程仍可能以 0 退出，所以必须看到「Added 1 user」和「result: ok」。
func (h *Helper) xrayAddUser(ctx context.Context, bin, server string, item userChange) error {
	if item.Protocol == "" {
		return fmt.Errorf("xray api adu: 入站 %s 没有协议", item.Tag)
	}
	if strings.TrimSpace(item.Email) == "" || strings.HasPrefix(item.Email, "-") {
		return fmt.Errorf("xray api adu: 邮箱不能作为命令参数")
	}
	settings := map[string]any{"clients": []json.RawMessage{item.Add}}
	if item.Protocol == "vless" {
		settings["decryption"] = "none"
	}
	body, err := json.Marshal(map[string]any{
		"inbounds": []any{map[string]any{
			"tag": item.Tag, "listen": "127.0.0.1", "port": 1, "protocol": item.Protocol, "settings": settings,
		}},
	})
	if err != nil {
		return err
	}
	text, err := runXrayAPI(ctx, bin, []string{"api", "adu", "--server=" + server, "-timeout=7", "stdin:"}, body)
	if err != nil {
		return fmt.Errorf("xray api adu: %w", err)
	}
	if !strings.Contains(text, "Added 1 user") || !strings.Contains(text, "result: ok") {
		return fmt.Errorf("xray api adu: %s", text)
	}
	return nil
}

// xrayRemoveUser 使用 `api rmu -tag <入站> <邮箱>`。只把字段放进 stdin 时，26.3.27 返回 inbound tag not specified。
func xrayRemoveArgs(server, tag, email string) []string {
	return []string{"api", "rmu", "--server=" + server, "-timeout=7", "-tag=" + tag, email}
}

func (h *Helper) xrayRemoveUser(ctx context.Context, bin, server, tag, email string) error {
	if tag == "" || strings.ContainsAny(tag, " \t\r\n") {
		return fmt.Errorf("xray api rmu: 入站标签无效")
	}
	if strings.TrimSpace(email) == "" || strings.HasPrefix(email, "-") || strings.ContainsAny(email, " \t\r\n") {
		return fmt.Errorf("xray api rmu: 邮箱不能作为命令参数")
	}
	text, err := runXrayAPI(ctx, bin, xrayRemoveArgs(server, tag, email), nil)
	if err != nil {
		return fmt.Errorf("xray api rmu: %w", err)
	}
	if strings.Contains(text, "inbound tag not specified") || !strings.Contains(text, "Removed 1 user") {
		return fmt.Errorf("xray api rmu: %s", text)
	}
	return nil
}

func (h *Helper) noteKernel(kernel, kind, traceID, reason string, pid *int, disconnects *int) {
	event := contracts.KernelEvent{At: time.Now().UTC(), Kernel: kernel, Kind: kind, Reason: reason, TraceID: traceID, PID: pid, Disconnects: disconnects}
	h.recordKernel(event)
}

func (h *Helper) recordKernel(event contracts.KernelEvent) {
	h.eventsMu.Lock()
	h.events = append(h.events, event)
	if len(h.events) > 200 {
		h.events = h.events[len(h.events)-200:]
	}
	h.eventsMu.Unlock()
	path := filepath.Join(h.P.CoresDir, "kernel-events.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return
	}
	defer f.Close()
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = f.Write(append(raw, '\n'))
}

func (h *Helper) peekKernelEvents() []contracts.KernelEvent {
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()
	return append([]contracts.KernelEvent(nil), h.events...)
}

func (h *Helper) takeKernelEvents() []contracts.KernelEvent {
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()
	if len(h.events) == 0 {
		return nil
	}
	out := append([]contracts.KernelEvent(nil), h.events...)
	h.events = nil
	return out
}
