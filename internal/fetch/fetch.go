// Package fetch 下载单个文件：边下边写 <目标>.part，中断后用 HTTP Range 续传；依次尝试多个地址，
// 多久没收到数据就算超时（不用整体时限）；完成后按 SHA-256 校验，不一致即丢弃并换下一个地址。
// 内核、dsh 与 Weir 自更新共用它。
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultIdle 是默认的无数据超时。
const DefaultIdle = 60 * time.Second

// 低速换源：连续 DefaultSlowFor 低于 DefaultMinSpeed 就放弃当前来源、换下一个（与 install-online.sh 的
// curl --speed-limit 102400 --speed-time 15 一致）。全部来源都试过后，回到只因为慢被放弃的来源里最快的那个，
// 不再限速、从断点下完，否则全部来源都慢（或最后一个连不上）时会一个也下不完。
const (
	DefaultMinSpeed = 100 << 10
	DefaultSlowFor  = 15 * time.Second
)

// ErrSlow 表示来源持续低于最低速度而被放弃；已下载的部分保留，下一个来源从断点续传。
var ErrSlow = errors.New("下载过慢")

// Source 是一个不经 HTTP 地址的来源（如经某台 VPS 中转）：Fetch 把完整内容写到 dest，按摘要校验由 Download 负责。
// 已下载的 .part 不能续传给这类来源，Fetch 从头写。
type Source struct {
	Name  string
	Fetch func(ctx context.Context, dest string, progress func(done, total int64)) error
	// Unmetered：来源有一段时间报不出进度（如 VPS 先自己下载、校验，再上传），不按低速换源规则放弃；
	// 卡住与超时由 Fetch 自己判断并返回原因。
	Unmetered bool
	// Before 为 k+1 时排在 URLs[k] 之前（管理员可以把经某台 VPS 中转排在直连前面）；为 0 时排在全部 URLs 之后。
	Before int
}

// step 是按顺序排好的一个来源：url 与 src 二选一。
type step struct {
	url string
	src *Source
}

// steps 按 Source.Before 把 Sources 插进 URLs，顺序即尝试顺序。
func (r Request) steps() []step {
	var out []step
	for k := 0; k <= len(r.URLs); k++ {
		for i := range r.Sources {
			b := r.Sources[i].Before
			if b == k+1 || b == 0 && k == len(r.URLs) || b > len(r.URLs) && k == len(r.URLs) {
				out = append(out, step{src: &r.Sources[i]})
			}
		}
		if k < len(r.URLs) {
			out = append(out, step{url: r.URLs[k]})
		}
	}
	return out
}

// Progress 是下载进度；Total 未知时为 0。
type Progress struct {
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Speed  int64  `json:"speed"` // 字节 / 秒，最近一秒
	Source string `json:"source"`
}

// Request 描述一次下载。URLs 与 Sources 内容必须相同（都按 SHA256 校验），依次尝试；Sources 按 Before 插在 URLs 之间。
// MinSpeed / SlowFor 为 0 时用默认的低速换源规则；MinSpeed < 0 关闭低速换源。
type Request struct {
	URLs       []string
	Sources    []Source
	Dest       string
	SHA256     string
	MaxSize    int64
	Idle       time.Duration
	MinSpeed   int64
	SlowFor    time.Duration
	HTTP       *http.Client // 不要设置整体 Timeout；超时由 Idle 控制
	OnProgress func(Progress)
	Log        func(string)
}

// ErrDigest 表示下载内容与摘要不一致。
var ErrDigest = errors.New("内容与摘要不一致")

// Download 执行下载，成功后 Dest 是校验过的文件；失败时保留 .part 供下次续传（摘要不一致时删除）。
func Download(ctx context.Context, r Request) error {
	if len(r.SHA256) != 64 {
		return errors.New("缺少摘要，拒绝下载")
	}
	if r.Idle <= 0 {
		r.Idle = DefaultIdle
	}
	if r.HTTP == nil {
		r.HTTP = &http.Client{}
	}
	if r.Log == nil {
		r.Log = func(string) {}
	}
	if r.MinSpeed == 0 {
		r.MinSpeed = DefaultMinSpeed
	}
	if r.SlowFor <= 0 {
		r.SlowFor = DefaultSlowFor
	}
	if ok, _ := matches(r.Dest, r.SHA256); ok {
		r.Log("已有校验通过的文件，跳过下载")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.Dest), 0o755); err != nil {
		return err
	}
	steps := r.steps()
	n := len(steps)
	if n == 0 {
		return errors.New("没有可用的下载地址")
	}
	last := errors.New("没有可用的下载地址")
	// 只因为慢被放弃的来源里最快的一个：全部来源都试过后回到它，不再限速，从断点下完。
	// 不能只给最后一个来源免限速：最后一个连不上时，前面那些慢但一直在下的来源就全白放弃了。
	best, bestSpeed := -1, int64(-1)
	for i := 0; i <= n; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		retry := i == n
		if retry && best < 0 {
			break
		}
		idx := i
		if retry {
			idx = best
		}
		st := steps[idx]
		var host string
		var err error
		if st.src == nil {
			host = Host(st.url)
		} else {
			host = st.src.Name
		}
		switch {
		case retry:
			r.Log("其他来源都不可用，回到最快的 " + host + "，不再限速")
		case i > 0:
			r.Log("改用 " + host)
		}
		// 只有一个来源时没有可换的，不按低速放弃。
		limit := r.MinSpeed > 0 && n > 1 && !retry
		started := time.Now()
		var before int64
		if st.src == nil {
			before = partSize(r.Dest)
			err = r.once(ctx, st.url, host, limit)
		} else {
			err = r.source(ctx, *st.src, limit && !st.src.Unmetered)
		}
		if errors.Is(err, ErrSlow) && !retry {
			got := partSize(r.Dest) - before
			if st.src != nil {
				got = partSize(r.Dest) // 非 HTTP 来源从头写
			}
			if sp := int64(float64(got) / time.Since(started).Seconds()); sp > bestSpeed {
				best, bestSpeed = idx, sp
			}
		}
		if err == nil {
			ok, sumErr := matches(r.Dest+".part", r.SHA256)
			if sumErr == nil && ok {
				if err = os.Rename(r.Dest+".part", r.Dest); err != nil {
					return err
				}
				r.Log("下载完成，摘要校验通过")
				return nil
			}
			_ = os.Remove(r.Dest + ".part")
			err = fmt.Errorf("%s：%w", host, ErrDigest)
		}
		if ctx.Err() != nil {
			r.Log("已取消，保留已下载的部分")
			return ctx.Err()
		}
		r.Log(err.Error())
		last = err
	}
	return last
}

// watchSlow 每秒看一次 got 的增量，连续 SlowFor 低于 MinSpeed 时调用 cancel；返回的函数报告是否因低速取消。
func (r Request) watchSlow(ctx context.Context, cancel context.CancelFunc, got *atomic.Int64) func() bool {
	var slow atomic.Bool
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		var below time.Duration
		prev := got.Load()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			cur := got.Load()
			if cur-prev < r.MinSpeed {
				below += time.Second
			} else {
				below = 0
			}
			prev = cur
			if below >= r.SlowFor {
				slow.Store(true)
				cancel()
				return
			}
		}
	}()
	return slow.Load
}

func partSize(dest string) int64 {
	if st, err := os.Stat(dest + ".part"); err == nil {
		return st.Size()
	}
	return 0
}

func (r Request) slowErr(host string, done int64) error {
	return fmt.Errorf("%s %w：连续 %s 低于 %s/s（已下载 %s）", host, ErrSlow, r.SlowFor, Size(r.MinSpeed), Size(done))
}

// source 从非 HTTP 来源完整下载到 .part（不续传）。
func (r Request) source(ctx context.Context, s Source, limit bool) error {
	part := r.Dest + ".part"
	_ = os.Remove(part)
	ictx, cancel := context.WithCancel(ctx)
	defer cancel()
	var got atomic.Int64
	slow := func() bool { return false }
	if limit {
		slow = r.watchSlow(ictx, cancel, &got)
	}
	r.Log("从 " + s.Name + " 下载")
	err := s.Fetch(ictx, part, func(done, total int64) {
		got.Store(done)
		if r.OnProgress != nil {
			r.OnProgress(Progress{Done: done, Total: total, Source: s.Name})
		}
	})
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case slow():
		return r.slowErr(s.Name, got.Load())
	}
	return fmt.Errorf("%s：%v", s.Name, trim(err))
}

// once 从一个地址下载（或续传）到 .part；返回 nil 表示该地址已给出完整内容。limit 为真时按低速换源规则放弃。
func (r Request) once(ctx context.Context, u, host string, limit bool) error {
	part := r.Dest + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	ictx, cancel := context.WithCancel(ctx)
	defer cancel()
	var got atomic.Int64
	slow := func() bool { return false }
	if limit {
		slow = r.watchSlow(ictx, cancel, &got)
	}
	req, err := http.NewRequestWithContext(ictx, "GET", u, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	timer := time.AfterFunc(r.Idle, cancel)
	defer timer.Stop()
	resp, err := r.HTTP.Do(req)
	if err != nil {
		if ctx.Err() == nil && slow() {
			return r.slowErr(host, have)
		}
		if ctx.Err() == nil && ictx.Err() != nil {
			return fmt.Errorf("%s 超过 %s 没有响应", host, r.Idle)
		}
		return fmt.Errorf("%s 连接失败：%v", host, trim(err))
	}
	defer resp.Body.Close()
	var f *os.File
	total := int64(0)
	switch {
	case resp.StatusCode == http.StatusPartialContent && have > 0:
		start, size, ok := contentRange(resp.Header.Get("Content-Range"))
		if !ok || start != have {
			return r.restart(host, "续传位置不符")
		}
		total = size
		r.Log(fmt.Sprintf("%s：从 %s 处续传", host, Size(have)))
		f, err = os.OpenFile(part, os.O_WRONLY|os.O_APPEND, 0o644)
	case resp.StatusCode == http.StatusOK:
		if have > 0 {
			r.Log(host + " 不支持续传，从头下载")
		} else {
			r.Log("从 " + host + " 下载")
		}
		have = 0
		if resp.ContentLength > 0 {
			total = resp.ContentLength
		}
		f, err = os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && have > 0:
		// 已下载完整但上次没来得及校验：交给上层校验。
		return nil
	default:
		return fmt.Errorf("%s 返回 HTTP %d", host, resp.StatusCode)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if r.MaxSize > 0 && total > r.MaxSize {
		return fmt.Errorf("%s 的文件过大（%s）", host, Size(total))
	}
	buf := make([]byte, 64<<10)
	done, window, windowStart := have, int64(0), time.Now()
	report := func(force bool) {
		if r.OnProgress == nil {
			return
		}
		if el := time.Since(windowStart); el >= time.Second || force {
			speed := int64(0)
			if el > 0 {
				speed = int64(float64(window) / el.Seconds())
			}
			r.OnProgress(Progress{Done: done, Total: total, Speed: speed, Source: host})
			window, windowStart = 0, time.Now()
		}
	}
	report(true)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			timer.Reset(r.Idle)
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			window += int64(n)
			got.Add(int64(n))
			if r.MaxSize > 0 && done > r.MaxSize {
				_ = f.Close()
				_ = os.Remove(part)
				return fmt.Errorf("%s 的文件超过 %s", host, Size(r.MaxSize))
			}
			report(false)
		}
		if rerr == io.EOF {
			report(true)
			if total > 0 && done < total {
				return fmt.Errorf("%s 提前断开（%s / %s）", host, Size(done), Size(total))
			}
			return nil
		}
		if rerr != nil {
			report(true)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if slow() {
				return r.slowErr(host, done)
			}
			if ictx.Err() != nil {
				return fmt.Errorf("%s 超过 %s 没有收到数据（已下载 %s）", host, r.Idle, Size(done))
			}
			return fmt.Errorf("%s 传输中断（已下载 %s）：%v", host, Size(done), trim(rerr))
		}
	}
}

func (r Request) restart(host, why string) error {
	_ = os.Remove(r.Dest + ".part")
	return fmt.Errorf("%s %s，下次从头下载", host, why)
}

// contentRange 解析 "bytes 100-199/1000"。
func contentRange(v string) (start, size int64, ok bool) {
	v = strings.TrimPrefix(v, "bytes ")
	dash, slash := strings.IndexByte(v, '-'), strings.IndexByte(v, '/')
	if dash <= 0 || slash <= dash {
		return 0, 0, false
	}
	start, err1 := strconv.ParseInt(v[:dash], 10, 64)
	size, err2 := strconv.ParseInt(v[slash+1:], 10, 64)
	if v[slash+1:] == "*" {
		size, err2 = 0, nil
	}
	return start, size, err1 == nil && err2 == nil
}

func matches(path, want string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want), nil
}

// Host 返回地址的主机名，用于日志。
func Host(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// Size 把字节数写成人看的大小。
func Size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func trim(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
