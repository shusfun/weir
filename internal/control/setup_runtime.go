package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/imzyb/MiGate/ent/user"
)

// startWorkerSet 是唯一启动后台任务的位置。测试可替换它，避免真的跑循环。
var startWorkerSet = func(s *Server, ctx context.Context) {
	go s.Workers(ctx)
}

func (s *Server) BindRuntime(ctx context.Context) {
	s.runtimeCtx = ctx
}

// StartBackground 在长驻进程里启动后台任务，或在未安装时监视终端安装结果。
func (s *Server) StartBackground(ctx context.Context) {
	s.BindRuntime(ctx)
	if s.Config.Installed {
		s.ensureWorkers()
	} else {
		go s.watchInstallation(ctx)
	}
}

func (s *Server) ensureWorkers() {
	if s.runtimeCtx == nil {
		return
	}
	s.workerOnce.Do(func() {
		startWorkerSet(s, s.runtimeCtx)
		s.workersReady.Store(true)
	})
}

func (s *Server) serviceReady() bool {
	if !s.Config.Installed || !s.workersReady.Load() || s.DB == nil || s.Redis == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.DB.PingContext(ctx) == nil && s.Redis.Ping(ctx).Err() == nil
}

// promoteInstalled 把已写盘的安装结果切进当前进程，并只启动一次后台任务。
func (s *Server) promoteInstalled() error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.Config.Installed && s.workersReady.Load() {
		return nil
	}
	cfg, err := LoadConfig(s.Path)
	if err != nil {
		return err
	}
	if !cfg.Installed {
		return errors.New("not installed")
	}
	if err = s.connect(cfg); err != nil {
		return err
	}
	ok, queryErr := s.Store.User.Query().Where(user.RoleEQ("admin")).Exist(context.Background())
	if queryErr != nil || !ok {
		return errors.New("installed database cannot be verified; restore configuration before starting")
	}
	s.Config = cfg
	s.ensureWorkers()
	return nil
}

func (s *Server) watchInstallation(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cred, err := readSetupCredential(s.Path)
			if err != nil || !cred.Installed {
				continue
			}
			if s.promoteInstalled() == nil {
				return
			}
		}
	}
}

func probeHost(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

func portListening(listen string) bool {
	address, err := probeHost(listen)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func postSetupVerify(listen, token string) (int, error) {
	address, err := probeHost(listen)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+address+"/api/v1/setup/verify", strings.NewReader("{}"))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

// RotateSetupToken 写入新凭证，并在已有服务时确认新凭证可用、旧凭证失效。
func RotateSetupToken(path string, cfg Config) (string, error) {
	previous := readSetupToken(path)
	token, err := Initialize(path, cfg)
	if err != nil {
		return "", err
	}
	if err = ConfirmTokenActive(cfg.Listen, token, previous); err != nil {
		return "", err
	}
	return token, nil
}

// ConfirmTokenActive 确认运行中的服务已经改用新凭证。服务没启动时不算失败。
func ConfirmTokenActive(listen, token, previous string) error {
	if !portListening(listen) {
		return nil
	}
	status, err := postSetupVerify(listen, token)
	if err != nil || status != http.StatusOK {
		return errors.New("running service did not accept the new initialization token")
	}
	if previous != "" && subtle.ConstantTimeCompare([]byte(previous), []byte(token)) != 1 {
		oldStatus, oldErr := postSetupVerify(listen, previous)
		if oldErr != nil || oldStatus != http.StatusUnauthorized {
			return errors.New("running service still accepts the previous initialization token")
		}
	}
	return nil
}

func localServiceReady(listen string) bool {
	address, err := probeHost(listen)
	if err != nil {
		return false
	}
	client := http.Client{Timeout: 2 * time.Second}
	health, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		return false
	}
	defer health.Body.Close()
	var healthBody struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(io.LimitReader(health.Body, 4096)).Decode(&healthBody) != nil || health.StatusCode != 200 || healthBody.Status != "ok" {
		return false
	}
	statusReq, err := http.NewRequest(http.MethodGet, "http://"+address+"/api/v1/setup/status", nil)
	if err != nil {
		return false
	}
	statusResp, err := client.Do(statusReq)
	if err != nil {
		return false
	}
	defer statusResp.Body.Close()
	var state struct {
		Installed bool `json:"installed"`
		Ready     bool `json:"ready"`
	}
	if json.NewDecoder(io.LimitReader(statusResp.Body, 4096)).Decode(&state) != nil || statusResp.StatusCode != 200 {
		return false
	}
	return state.Installed && state.Ready
}

func waitLocalService(listen string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if localServiceReady(listen) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("installed but the running service did not become ready")
}
