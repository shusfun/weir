package gate

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shusfun/weir/packages/contracts"
)

// socket 是 /proc/net 中一个处于监听（TCP LISTEN）或已绑定未连接（UDP）状态的套接字。
type socket struct {
	port  int
	proto string
	inode string
}

// parseProcNet 解析 /proc/net/{tcp,tcp6,udp,udp6} 的内容。TCP 取状态 0A（LISTEN），
// UDP 取远端为全零的已绑定套接字（07）。
func parseProcNet(text, proto string) []socket {
	var out []socket
	sc := bufio.NewScanner(strings.NewReader(text))
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 10 {
			continue
		}
		local, remote, state := f[1], f[2], f[3]
		if proto == "tcp" && state != "0A" {
			continue
		}
		if proto == "udp" && (state != "07" || !strings.HasSuffix(remote, ":0000")) {
			continue
		}
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		port, err := strconv.ParseInt(local[i+1:], 16, 32)
		if err != nil || port == 0 {
			continue
		}
		out = append(out, socket{port: int(port), proto: proto, inode: f[9]})
	}
	return out
}

// listening 返回本机正在监听的端口，以及每个端口所属进程的 PID（root 才能看到全部进程）。
func listening(procRoot string) ([]contracts.ListeningPort, map[int][]int) {
	var socks []socket
	for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
		b, err := os.ReadFile(filepath.Join(procRoot, "net", name))
		if err != nil {
			continue
		}
		socks = append(socks, parseProcNet(string(b), strings.TrimSuffix(name, "6"))...)
	}
	owner := inodeOwners(procRoot)
	seen := map[string]bool{}
	var ports []contracts.ListeningPort
	pids := map[int][]int{}
	for _, s := range socks {
		key := s.proto + ":" + strconv.Itoa(s.port)
		pid := owner[s.inode]
		if pid > 0 {
			pids[s.port] = append(pids[s.port], pid)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		ports = append(ports, contracts.ListeningPort{Port: s.port, Proto: s.proto, Process: processName(procRoot, pid)})
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Proto < ports[j].Proto
	})
	return ports, pids
}

// inodeOwners 通过 /proc/<pid>/fd 把套接字 inode 映射到进程。无权限读取的进程跳过（显示为未知）。
func inodeOwners(procRoot string) map[string]int {
	out := map[string]int{}
	dirs, _ := os.ReadDir(procRoot)
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(procRoot, d.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(procRoot, d.Name(), "fd", fd.Name()))
			if err == nil && strings.HasPrefix(link, "socket:[") {
				out[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = pid
			}
		}
	}
	return out
}

func processName(procRoot string, pid int) string {
	if pid <= 0 {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// conflicts 返回 want 中被「非本项目内核」进程占用的第一个端口；ours 是本项目内核的 PID。
func conflicts(want []int, pids map[int][]int, ours map[int]bool) int {
	for _, port := range want {
		for _, pid := range pids[port] {
			if !ours[pid] {
				return port
			}
		}
	}
	return 0
}
