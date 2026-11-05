package gate

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const baotaDefaultConfig = "/www/server/panel/vhost/nginx/0.default.conf"
const baotaDefaultRoot = "/www/server/nginx/html"

// 仅识别宝塔标准默认站点，不猜自定义反代或改写规则。验证内容经实际 HTTP 读取后才上报成功。
func baotaHTTPRoot(configPath, root string) error {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.SplitN(l, "#", 2)[0]
		lines = append(lines, l)
	}
	conf := strings.Join(lines, "\n")
	if !regexp.MustCompile(`(?m)^\s*listen\s+80\s*;`).MatchString(conf) || !regexp.MustCompile(`(?m)^\s*server_name\s+_\s*;`).MatchString(conf) {
		return errors.New("宝塔默认站点没有标准 HTTP 入口")
	}
	roots := regexp.MustCompile(`(?m)^\s*root\s+([^;]+);`).FindAllStringSubmatch(conf, -1)
	if len(roots) != 1 || strings.TrimSpace(roots[0][1]) != root {
		return errors.New("宝塔默认站点目录不是受支持的标准目录")
	}
	if regexp.MustCompile(`\b(location|return|rewrite|proxy_pass|include)\b`).MatchString(conf) {
		return errors.New("宝塔默认站点有自定义路由，不能自动使用 HTTP 验证")
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real != root {
		return errors.New("宝塔验证目录不存在或包含符号链接")
	}
	return nil
}

func serveBaotaChallenge(root, token, keyAuth, host string, seconds int) error {
	base, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer base.Close()
	for _, dir := range []string{".well-known", ".well-known/acme-challenge"} {
		if err = base.Mkdir(dir, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		st, err := base.Lstat(dir)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("验证目录不是普通目录")
		}
	}
	path := ".well-known/acme-challenge/" + token
	f, err := base.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(keyAuth); err != nil {
		f.Close()
		base.Remove(path)
		return err
	}
	if err = f.Close(); err != nil {
		base.Remove(path)
		return err
	}
	cleanup := func() {
		r, e := os.OpenRoot(root)
		if e != nil {
			return
		}
		defer r.Close()
		b, e := r.ReadFile(path)
		if e == nil && string(b) == keyAuth {
			_ = r.Remove(path)
		}
	}
	req, _ := http.NewRequest("GET", "http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(acmePort))+"/.well-known/acme-challenge/"+token, nil)
	req.Host = host
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		cleanup()
		return fmt.Errorf("宝塔 HTTP 验证路径不可达：%w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	client.CloseIdleConnections()
	if readErr != nil || resp.StatusCode != 200 || string(body) != keyAuth {
		cleanup()
		return errors.New("宝塔默认站点没有返回本次验证内容")
	}
	time.AfterFunc(time.Duration(seconds)*time.Second, cleanup)
	return nil
}
