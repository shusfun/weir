package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/ghrelease"
	"github.com/shusfun/weir/packages/contracts"
)

const (
	assetMaxSize   = 512 << 20
	probeMaxBytes  = 4 << 20
	probeDefault   = 1 << 20
	probeTimeout   = 30 * time.Second
	assetTimeout   = 2 * time.Hour
	uploadAttempts = 5 // 同一位置连续失败几次就放弃；有进展时重新计数
)

var uploadID = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// convert 经 JSON 在动作参数（map）与约定结构之间转换。
func convert(in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// githubURLs 检查中心给的下载地址：只接受 GitHub 的 https 地址，或「合法镜像前缀 + 这样的地址」，
// 避免把 Gate 当成访问任意地址的跳板。
func (a *Agent) githubURLs(urls []string) ([]string, error) {
	if len(urls) == 0 || len(urls) > 20 {
		return nil, errors.New("下载地址必须是 1-20 个")
	}
	gh := ghrelease.Client{Host: a.ghHost}
	for _, raw := range urls {
		if gh.Allowed(raw) == nil {
			continue
		}
		// 镜像形式：前缀里不会再有 https://，原地址从最后一个 https:// 开始。
		i := strings.LastIndex(raw, "https://")
		if i <= 0 || !ghrelease.ValidPrefix(raw[:i]) || gh.Allowed(raw[i:]) != nil {
			return nil, fmt.Errorf("下载地址 %s 不是 GitHub 地址或其镜像地址，拒绝", fetch.Host(raw))
		}
	}
	return urls, nil
}

// assetFetch 替中心下载 GitHub 文件：校验后存到 <数据目录>/assets/，再分块上传给中心；临时文件用完即删。
func (a *Agent) assetFetch(ctx context.Context, p map[string]any) (map[string]any, error) {
	var req contracts.AssetFetch
	if err := convert(p, &req); err != nil {
		return nil, errors.New("参数无效")
	}
	switch {
	case !uploadID.MatchString(req.UploadID):
		return nil, errors.New("上传编号无效")
	case len(req.SHA256) != 64:
		return nil, errors.New("缺少摘要")
	case req.MaxSize < 0 || req.MaxSize > assetMaxSize:
		return nil, fmt.Errorf("大小上限不能超过 %s", fetch.Size(assetMaxSize))
	}
	limit := req.MaxSize
	if limit == 0 {
		limit = assetMaxSize
	}
	if req.Size < 0 || req.Size > limit {
		return nil, fmt.Errorf("文件大小 %s 超过上限 %s", fetch.Size(req.Size), fetch.Size(limit))
	}
	urls, err := a.githubURLs(req.URLs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, assetTimeout)
	defer cancel()
	dir := filepath.Join(a.P.Data, "assets")
	dest := filepath.Join(dir, req.UploadID)
	defer os.Remove(dest)
	defer os.Remove(dest + ".part")
	err = fetch.Download(ctx, fetch.Request{URLs: urls, Dest: dest, SHA256: req.SHA256, MaxSize: limit, HTTP: a.externalHTTP(),
		Log: func(s string) { log.Printf("asset %s: %s", req.UploadID, s) }})
	if err != nil {
		return nil, fmt.Errorf("下载失败：%w", err)
	}
	if req.Size > 0 {
		if st, err := os.Stat(dest); err == nil && st.Size() != req.Size {
			return nil, fmt.Errorf("下载的文件大小 %d 与中心给的 %d 不一致", st.Size(), req.Size)
		}
	}
	n, err := a.uploadFile(ctx, req.UploadID, dest)
	if err != nil {
		return nil, fmt.Errorf("上传到中心失败：%w", err)
	}
	return map[string]any{"size": n}, nil
}

// uploadFile 从中心已收到的位置开始分块上传；中心回 409 时按它报的位置续传，其他失败退避重试。
func (a *Agent) uploadFile(ctx context.Context, id, path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	buf := make([]byte, contracts.UploadChunk)
	backoff := a.uploadBackoff
	if backoff <= 0 {
		backoff = time.Second
	}
	offset, known, fails := int64(0), false, 0
	var last error
	retry := func(err error) error {
		last, known = err, false
		if fails++; fails >= uploadAttempts {
			return fmt.Errorf("连续 %d 次失败：%w", fails, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff << (fails - 1)):
		}
		return nil
	}
	for {
		if !known {
			got, err := a.center.uploadState(ctx, id)
			if err != nil {
				if e := retry(err); e != nil {
					return 0, e
				}
				continue
			}
			offset, known = got.Received, true
		}
		if offset > size {
			return 0, fmt.Errorf("中心报告已收到 %d 字节，超过文件大小 %d", offset, size)
		}
		if offset == size {
			return size, nil
		}
		n, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return 0, err
		}
		code, got, err := a.center.uploadChunk(ctx, id, offset, buf[:n])
		switch {
		case err == nil:
			if got.Received <= offset {
				if e := retry(fmt.Errorf("中心没有接收位置 %d 的数据", offset)); e != nil {
					return 0, e
				}
				continue
			}
			offset, fails = got.Received, 0
		case code == http.StatusConflict:
			// 位置不符（例如上一块已写入但回复丢失）：按中心报的位置继续；没有进展才算一次失败。
			if got.Received > offset {
				fails = 0
			} else if e := retry(fmt.Errorf("中心要求从 %d 续传", got.Received)); e != nil {
				return 0, e
			}
			offset, known = got.Received, true
		default:
			if e := retry(err); e != nil {
				return 0, e
			}
		}
	}
}

// sourceProbe 测一个下载来源：用 Range 取前 Bytes 字节，记录首字节延迟与速度，整体不超过 30 秒。
func (a *Agent) sourceProbe(ctx context.Context, p map[string]any) (map[string]any, error) {
	var req contracts.SourceProbe
	if err := convert(p, &req); err != nil {
		return nil, errors.New("参数无效")
	}
	if req.Bytes == 0 {
		req.Bytes = probeDefault
	}
	if req.Bytes < 0 || req.Bytes > probeMaxBytes {
		return nil, fmt.Errorf("测速字节数必须在 1 到 %s 之间", fetch.Size(probeMaxBytes))
	}
	if _, err := a.githubURLs([]string{req.URL}); err != nil {
		return nil, err
	}
	res := probeSource(ctx, a.externalHTTP(), req)
	out := map[string]any{}
	_ = convert(res, &out)
	if res.Error != "" {
		return out, errors.New(res.Error)
	}
	return out, nil
}

func probeSource(ctx context.Context, client *http.Client, req contracts.SourceProbe) contracts.SourceProbeResult {
	var res contracts.SourceProbeResult
	host := fetch.Host(req.URL)
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	start := time.Now()
	var first time.Time
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotFirstResponseByte: func() { first = time.Now() }})
	hr, err := http.NewRequestWithContext(ctx, "GET", req.URL, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	hr.Header.Set("Range", "bytes=0-"+strconv.FormatInt(req.Bytes-1, 10))
	resp, err := client.Do(hr)
	if err != nil {
		res.Error = host + " 连接失败：" + err.Error()
		if ctx.Err() != nil {
			res.Error = fmt.Sprintf("%s %s 内没有响应", host, probeTimeout)
		}
		return res
	}
	defer resp.Body.Close()
	if first.IsZero() {
		first = time.Now()
	}
	res.TTFBMs = first.Sub(start).Milliseconds()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		res.Error = fmt.Sprintf("%s 返回 HTTP %d", host, resp.StatusCode)
		return res
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, req.Bytes))
	res.Bytes = n
	// 速度按从发起请求到收完的总耗时算（含首字节延迟），与实际下载一个小文件的体验一致；计时精度不足时按 1 毫秒算。
	res.Speed = int64(float64(n) / max(time.Since(start), time.Millisecond).Seconds())
	switch {
	case err != nil && ctx.Err() != nil:
		res.Error = fmt.Sprintf("%s %s 内没有传完（已收到 %s）", host, probeTimeout, fetch.Size(n))
	case err != nil:
		res.Error = fmt.Sprintf("%s 传输中断（已收到 %s）：%v", host, fetch.Size(n), err)
	}
	return res
}
