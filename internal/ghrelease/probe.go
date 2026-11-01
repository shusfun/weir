package ghrelease

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ProbeResult 是一次测速：TTFB 为首字节延迟，Speed 为字节 / 秒；失败时 Err 为原因（已取到的部分仍计入 Bytes）。
type ProbeResult struct {
	TTFB  time.Duration
	Bytes int64
	Speed int64
	Err   error
}

// Probe 用 Range 取 u 的前 n 字节测速（中心测直连与镜像、Gate 测自己直连 GitHub 都用它）。
// 来源不支持 Range、返回整个文件时只读前 n 字节。整个测速最多 timeout。
func Probe(ctx context.Context, client *http.Client, u string, n int64, timeout time.Duration) ProbeResult {
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return ProbeResult{Err: err}
	}
	req.Header.Set("Range", "bytes=0-"+strconv.FormatInt(n-1, 10))
	req.Header.Set("User-Agent", "Weir")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ProbeResult{Err: fmt.Errorf("%s 内没有响应", timeout)}
		}
		return ProbeResult{Err: fmt.Errorf("连接失败：%v", short(err))}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return ProbeResult{TTFB: time.Since(start), Err: fmt.Errorf("返回 HTTP %d", resp.StatusCode)}
	}
	buf := make([]byte, 32<<10)
	var got int64
	var ttfb time.Duration
	for got < n {
		k, rerr := resp.Body.Read(buf[:min(int64(len(buf)), n-got)])
		if k > 0 && ttfb == 0 {
			ttfb = time.Since(start)
		}
		got += int64(k)
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			res := ProbeResult{TTFB: ttfb, Bytes: got, Err: fmt.Errorf("传输中断：%v", short(rerr))}
			if ctx.Err() != nil {
				res.Err = fmt.Errorf("%s 内只取到 %d 字节", timeout, got)
			}
			res.Speed = speed(got, time.Since(start)-ttfb)
			return res
		}
	}
	if got == 0 {
		return ProbeResult{TTFB: time.Since(start), Err: errors.New("没有返回内容")}
	}
	return ProbeResult{TTFB: ttfb, Bytes: got, Speed: speed(got, time.Since(start)-ttfb)}
}

// speed 按首字节之后的时间算速度：首字节延迟单独显示，不摊进速度里。
func speed(n int64, d time.Duration) int64 {
	if d < time.Millisecond {
		d = time.Millisecond
	}
	return int64(float64(n) / d.Seconds())
}

func short(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// Name 是来源给人看的名称：直连写 github.com，镜像写主机名；gate 来源由调用方换成 VPS 名称。
func (s Source) Name() string {
	switch s.Kind {
	case SourceDirect:
		return "github.com"
	case SourceMirror:
		if u, err := url.Parse(s.Prefix); err == nil && u.Host != "" {
			return u.Host
		}
		return s.Prefix
	}
	return "VPS " + strconv.FormatInt(s.GateID, 10)
}

// HTTPOnly 去掉 gate 来源，顺序不变（交给 root 一侧、浏览器与 Gate 的列表只含 HTTP 来源）。
func HTTPOnly(list []Source) []Source {
	out := make([]Source, 0, len(list))
	for _, s := range list {
		if s.Kind == SourceDirect || s.Kind == SourceMirror {
			out = append(out, s)
		}
	}
	return out
}

// HostMirror 返回形如 https://主机/ 的镜像的主机名（安装脚本的 WEIR_GITHUB_MIRRORS 只接受主机名）；其他前缀返回空。
func HostMirror(s Source) string {
	if s.Kind != SourceMirror {
		return ""
	}
	u, err := url.Parse(s.Prefix)
	if err != nil || u.Scheme != "https" || u.Path != "/" || u.Port() != "" || !validHost(u.Host) {
		return ""
	}
	return u.Host
}
