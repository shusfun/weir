package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shusfun/weir/internal/kernelconf"
)

// defaultArgs 是驱动没有给出参数时（旧状态文件）的运行与自检参数。
func defaultArgs(name string) kernelconf.KernelArgs {
	if name == "sing-box" {
		return kernelconf.KernelArgs{Run: []string{"run", "-c", "{config}"}, Check: []string{"check", "-c", "{config}"}}
	}
	return kernelconf.KernelArgs{Run: []string{"run", "-config", "{config}"}, Check: []string{"run", "-test", "-config", "{config}"}}
}

// kernelArgs 返回内核的运行参数与配置自检参数（{config} 已换成路径）。参数来自驱动，必须通过校验。
func kernelArgs(name string, a kernelconf.KernelArgs, config string) (run, test []string, err error) {
	if len(a.Run) == 0 && len(a.Check) == 0 {
		a = defaultArgs(name)
	}
	if err = kernelconf.CheckArgs(a); err != nil {
		return nil, nil, err
	}
	return kernelconf.Args(a.Run, config), kernelconf.Args(a.Check, config), nil
}

// args 读取某内核已应用的参数（与配置文件放在一起）；没有时用默认参数。
func (s *Supervisor) args(name string) kernelconf.KernelArgs {
	var a kernelconf.KernelArgs
	if b, err := os.ReadFile(filepath.Join(s.p.CoresDir, name+".args.json")); err == nil {
		_ = json.Unmarshal(b, &a)
	}
	return a
}

// SetArgs 保存某内核的运行参数（切换配置时写入，Start 时读取）。
func (s *Supervisor) SetArgs(name string, a kernelconf.KernelArgs) error {
	if err := kernelconf.CheckArgs(a); err != nil {
		return err
	}
	b, _ := json.Marshal(a)
	return os.WriteFile(filepath.Join(s.p.CoresDir, name+".args.json"), b, 0640)
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
		run, _, err := kernelArgs(pr.name, s.args(pr.name), s.config(pr.name))
		if err != nil {
			if first {
				started <- err
			}
			return
		}
		cmd := exec.Command(bin, run...)
		dropPrivileges(cmd, s.p.RunAs)
		out, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		err = cmd.Start()
		// 先记下进程再通知已启动：紧接着的下一次应用要据此认出端口是自己的内核占着，不当成别的程序。
		if err == nil {
			s.mu.Lock()
			pr.cmd = cmd
			s.mu.Unlock()
		}
		if first {
			started <- err
			first = false
		}
		if err == nil {
			s.collect(pr, out)
			err = cmd.Wait()
		}
		s.mu.Lock()
		stopped := pr.stopped
		last := ""
		if n := len(pr.logs); n > 0 {
			last = pr.logs[n-1]
		}
		s.mu.Unlock()
		if stopped {
			return
		}
		// 内核输出只留在内存（重启辅助程序就没了），退出原因要写进服务日志；IP 与诊断接口一样遮盖。
		log.Printf("%s exited (%v), restarting in %s; last output: %s", pr.name, err, backoff, maskIPs(last))
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
func (s *Supervisor) Test(ctx context.Context, name, version, config string, a kernelconf.KernelArgs) error {
	bin := filepath.Join(s.p.KernelsDir, name+"-"+version)
	if _, err := os.Stat(bin); err != nil {
		return errors.New(name + " " + version + " is not installed")
	}
	_, test, err := kernelArgs(name, a, config)
	if err != nil {
		return err
	}
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
		if text == "" {
			text = err.Error() // 内核没能启动（如降权失败）时没有输出，交回启动错误本身
		}
		return errors.New(name + " 配置自检失败：" + text)
	}
	return nil
}
