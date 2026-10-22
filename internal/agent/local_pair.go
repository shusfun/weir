package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"
)

// statusError 保留中心返回的 HTTP 状态，调用方据此区分「稍后重试」与「明确拒绝」。
type statusError struct{ code int }

func (e statusError) Error() string { return fmt.Sprintf("center HTTP %d", e.code) }

// loopbackCenter 只接受指向本机回环地址的明文 HTTP 中心，用于单机角色。
func loopbackCenter(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LocalPair 用安装器写入的一次性凭证向本机中心配对。中心尚未初始化或暂不可用时
// 持续重试；凭证被拒绝时立即返回。成功后写入设备配置并删除凭证文件。
func LocalPair(ctx context.Context, center, name, path, tokenFile string) error {
	if !loopbackCenter(center) {
		return errors.New("local-pair requires a loopback http:// center")
	}
	center = strings.TrimRight(center, "/")
	if _, err := os.Stat(path); err == nil {
		return nil // 已配对：重复执行无副作用
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return errors.New("local pair token unavailable")
	}
	localToken := strings.TrimSpace(string(raw))
	if len(localToken) < 32 {
		return errors.New("local pair token invalid")
	}
	if name == "" {
		name, _ = os.Hostname()
		if name == "" {
			name = "local"
		}
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	c := Config{Center: center, Name: name, PrivateKey: base64.StdEncoding.EncodeToString(private), Socket: "/run/migate/helper.sock", DataDir: "/var/lib/migate-gate"}
	client, err := centerClient(c, 15*time.Second)
	if err != nil {
		return err
	}
	a := &Agent{Config: c, HTTP: client}
	for {
		var result map[string]any
		err = a.request(ctx, "POST", "/devices/pair", map[string]any{"name": name, "public_key": base64.StdEncoding.EncodeToString(pub), "local_token": localToken}, &result, false)
		if err == nil {
			id, ok := result["device_id"].(float64)
			if !ok || result["status"] != "approved" {
				return errors.New("local pair not approved")
			}
			c.DeviceID = int(id)
			if err = Save(path, c); err != nil {
				return err
			}
			if err = shareWithGateGroup(path); err != nil {
				return err
			}
			_ = os.Remove(tokenFile)
			return nil
		}
		var status statusError
		if errors.As(err, &status) && status.code != 503 && status.code != 429 && status.code < 500 {
			return fmt.Errorf("local pair refused: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// shareWithGateGroup 让以 migate-gate 身份运行的 Agent 能读取 root 写入的设备配置。
func shareWithGateGroup(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	group, err := user.LookupGroup("migate-gate")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	if err = os.Chown(path, 0, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0640)
}
