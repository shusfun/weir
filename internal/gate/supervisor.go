package gate

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// kernelArgs 返回内核的运行参数与配置自检参数。
func kernelArgs(name, config string) (run, test []string) {
	if name == "sing-box" {
		return []string{"run", "-c", config}, []string{"check", "-c", config}
	}
	return []string{"run", "-config", config}, []string{"run", "-test", "-config", config}
}

// proc 是一个被托管的内核进程。
type proc struct {
	name    string
	cmd     *exec.Cmd
	done    chan struct{}
	stopped bool
	logs    []string
}

// Supervisor 以子进程托管内核：崩溃后按退避重启；停止时不再拉起；保留最近 200 行输出供诊断。
type Supervisor struct {
	p     Paths
	mu    sync.Mutex
	procs map[string]*proc
}

func NewSupervisor(p Paths) *Supervisor { return &Supervisor{p: p, procs: map[string]*proc{}} }

func (s *Supervisor) binary(name string) (string, error) {
	v, err := os.ReadFile(filepath.Join(s.p.KernelsDir, name+".version"))
	if err != nil {
		return "", errors.New(name + " is not installed")
	}
	return filepath.Join(s.p.KernelsDir, name+"-"+strings.TrimSpace(string(v))), nil
}

func (s *Supervisor) config(name string) string { return filepath.Join(s.p.CoresDir, name+".json") }

// Start 启动（或重启）某个内核，并持续守护直到 Stop。
func (s *Supervisor) Start(name string) error {
	s.Stop(name)
	bin, err := s.binary(name)
	if err != nil {
		return err
	}
	pr := &proc{name: name, done: make(chan struct{})}
	s.mu.Lock()
	s.procs[name] = pr
	s.mu.Unlock()
	started := make(chan error, 1)
	go s.keep(pr, bin, started)
	return <-started
}

func (s *Supervisor) keep(pr *proc, bin string, started chan<- error) {
	defer close(pr.done)
	backoff := time.Second
	first := true
	for {
		run, _ := kernelArgs(pr.name, s.config(pr.name))
		cmd := exec.Command(bin, run...)
		dropPrivileges(cmd, s.p.RunAs)
		out, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		err := cmd.Start()
		if first {
			started <- err
			first = false
		}
		if err == nil {
			s.mu.Lock()
			pr.cmd = cmd
			s.mu.Unlock()
			s.collect(pr, out)
			_ = cmd.Wait()
		}
		s.mu.Lock()
		stopped := pr.stopped
		s.mu.Unlock()
		if stopped {
			return
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (s *Supervisor) collect(pr *proc, r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s.mu.Lock()
		pr.logs = append(pr.logs, sc.Text())
		if len(pr.logs) > 200 {
			pr.logs = pr.logs[len(pr.logs)-200:]
		}
		s.mu.Unlock()
	}
}

// Stop 停止内核且不再自动拉起。
func (s *Supervisor) Stop(name string) {
	s.mu.Lock()
	pr := s.procs[name]
	if pr != nil {
		pr.stopped = true
		if pr.cmd != nil && pr.cmd.Process != nil {
			_ = pr.cmd.Process.Kill()
		}
		delete(s.procs, name)
	}
	s.mu.Unlock()
	if pr != nil {
		select {
		case <-pr.done:
		case <-time.After(10 * time.Second):
		}
	}
}

// Running 返回正在运行的内核及其 PID。
func (s *Supervisor) Running() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for name, pr := range s.procs {
		if pr.cmd != nil && pr.cmd.Process != nil && pr.cmd.ProcessState == nil {
			out[name] = pr.cmd.Process.Pid
		}
	}
	return out
}

func (s *Supervisor) Logs(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pr := s.procs[name]; pr != nil {
		return append([]string(nil), pr.logs...)
	}
	return nil
}

// Test 用指定版本内核自身的自检命令检查配置文件（版本切换前先检查，未通过不切换）。
func (s *Supervisor) Test(ctx context.Context, name, version, config string) error {
	bin := filepath.Join(s.p.KernelsDir, name+"-"+version)
	if _, err := os.Stat(bin); err != nil {
		return errors.New(name + " " + version + " is not installed")
	}
	_, test := kernelArgs(name, config)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, test...)
	dropPrivileges(cmd, s.p.RunAs)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if len(text) > 800 {
			text = text[len(text)-800:]
		}
		return errors.New(name + " 配置自检失败：" + text)
	}
	return nil
}
