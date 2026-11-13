package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/ghrelease"
)

// installerPath 是安装时保存在本机的 install.sh；Gate 安装包直接使用它，保证与中心同版本。
const installerPath = "/usr/local/lib/weir/install.sh"

// gateScript 是「添加 Gate」生成的一行命令所执行的脚本。凭证只作为参数传入，不放进 URL。
// 正式构建先从 GitHub 下载与中心同版本的发布包（按「GitHub 加速」：直连的位置由中心写入，镜像主机名来自
// 命令里的 WEIR_GITHUB_MIRRORS，与 install-online.sh 同一规则），按中心从 GitHub API 读到的官方摘要与包内
// SHA256SUMS 校验；没有发布信息或都失败时从中心下载。
const gateScript = `#!/bin/sh
# Weir Gate 一键安装：下载与中心同版本的程序，安装 Gate 角色，并用一次性凭证配对。
set -eu
token=''
laya=no
gate_id=''
public_key=''
while [ $# -gt 0 ]; do
 case "$1" in --token) token=$2; shift 2;; --laya) laya=yes; shift;; --gate-id) gate_id=$2; shift 2;; --public-key) public_key=$2; shift 2;; *) echo invalid_argument >&2;exit 2;; esac
done
[ -n "$token" ] || [ -n "$gate_id" ] || { echo 'usage: sh gate.sh --token TOKEN' >&2; exit 2; }
[ "$(id -u)" = 0 ] || { echo 'root required' >&2; exit 1; }
if [ -f /etc/weir/role ]; then
 case "$(cat /etc/weir/role)" in center|standalone) echo 'This host runs a center. Enable its local execution agent in the console; remote Gate installation cannot replace its identity.' >&2; exit 1;; esac
fi
case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo 'unsupported architecture' >&2; exit 1;; esac
work=$(mktemp -d /tmp/weir-gate-XXXXXX)
trap 'rm -rf "$work"' EXIT
case "$arch" in amd64) raw='%[2]s'; sum='%[3]s';; *) raw='%[4]s'; sum='%[5]s';; esac
direct='%[6]s'
got=no
# 下载一个来源；低于 100 KiB/s 持续 15 秒就换下一个，最后从中心下载不限速
try_github() {
 curl -fsSL --connect-timeout 10 --speed-limit 102400 --speed-time 15 --proto '=https' -o "$work/gate.tar.gz" "$1" || return 1
 [ "$(sha256sum "$work/gate.tar.gz" | cut -d' ' -f1)" = "$sum" ]
}
if [ -n "$raw" ] && command -v sha256sum >/dev/null 2>&1; then
 urls=''
 [ "$direct" = first ] && urls="$raw"
 mirrors=${WEIR_GITHUB_MIRRORS:-}
 [ "$mirrors" = off ] && mirrors=''
 for m in $(echo "$mirrors" | tr ',' ' '); do
  case "$m" in *[!A-Za-z0-9.-]*|-*|'') echo "WEIR_GITHUB_MIRRORS 只能是逗号分隔的主机名" >&2; exit 1;; esac
  urls="$urls https://$m/$raw"
 done
 [ "$direct" = last ] && urls="$urls $raw"
 for u in $urls; do
  echo "Downloading $u"
  if try_github "$u"; then got=yes; break; fi
  echo "  failed or checksum mismatch, trying next source" >&2
 done
fi
if [ "$got" = yes ]; then
 tar -xzf "$work/gate.tar.gz" -C "$work"
 (cd "$work" && sha256sum -c --quiet SHA256SUMS) || { echo 'package checksum mismatch' >&2; exit 1; }
else
 curl -fsSL "%[1]s/install/gate.tar.gz?arch=$arch" -o "$work/gate.tar.gz"
 tar -xzf "$work/gate.tar.gz" -C "$work"
fi
chmod 0755 "$work/weir"
if [ -n "$gate_id" ]; then
 # Check the existing identity using this verified program before changing services.
 "$work/weir" laya prepare gate --gate-id "$gate_id" --public-key "$public_key" --center '%[1]s'
 sh "$work/install.sh" gate
 systemctl restart weir-gate.service
 echo 'Laya host prepared; existing device identity preserved.'
 exit 0
fi
WEIR_GATE_PAIRING=1 sh "$work/install.sh" gate
systemctl stop weir-gate.service
if ! /opt/weir/current/weir gate pair --replace --center '%[1]s' --token "$token" --config /etc/weir/gate/agent.json; then
 if [ -e /etc/weir/gate/agent.json.pairing ]; then
  echo 'Device identity replacement is incomplete; Gate remains stopped. Fix the reported local error and retry adding this VPS.' >&2
 elif [ -f /etc/weir/gate/agent.json ]; then
  systemctl restart weir-gate.service || { echo 'Failed to restore the previous Gate service.' >&2; exit 1; }
 fi
 exit 1
fi
if [ "$laya" = yes ]; then /opt/weir/current/weir laya prepare gate; fi
systemctl enable weir-gate.service
systemctl restart weir-gate.service
echo 'Gate paired; it will appear online in the center within a minute.'
`

func (s *Server) installRoutes(r *gin.Engine) {
	r.GET("/install/gate.sh", func(c *gin.Context) {
		if !s.Config.Installed {
			failure(c, 503, "setup_required")
			return
		}
		origin := s.siteOrigin(c)
		args := []any{origin, "", "", "", "", "no"}
		if s.Store != nil {
			args = s.gateScriptArgs(c.Request.Context(), origin)
		}
		c.Data(200, "text/x-shellscript; charset=utf-8", []byte(fmt.Sprintf(gateScript, args...)))
	})
	r.GET("/install/gate.tar.gz", s.gateBundle)
}

// gateScriptArgs 是 gate.sh 的参数：中心地址、两个架构发布包的 GitHub 地址与官方摘要、直连排在镜像前还是后。
// 发布信息最多等 5 秒（通常已缓存）；取不到时地址为空，脚本直接从中心下载。
func (s *Server) gateScriptArgs(ctx context.Context, origin string) []any {
	args := []any{origin, "", "", "", "", "no"}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	assets, _ := s.weirReleaseAssets(wctx, true)
	cancel()
	for i, arch := range []string{"amd64", "arm64"} {
		// 地址写进单引号里：只接受 URL 安全字符，防止发布信息里的引号改变脚本
		if a, ok := assets[arch]; ok && scriptSafeURL.MatchString(a.URL) && sha256Hex.MatchString(a.SHA256) {
			args[1+2*i], args[2+2*i] = a.URL, a.SHA256
		}
	}
	if set, err := s.githubSources(ctx); err == nil {
		args[5] = directPosition(set.Sources)
	}
	return args
}

var scriptSafeURL = regexp.MustCompile(`^https://[A-Za-z0-9.:-]+/[A-Za-z0-9._~/%+-]+$`)

// directPosition：直连排在第一个主机名镜像之前为 first，之后为 last，没有直连为 no。
// 安装命令只能带主机名形式的镜像（WEIR_GITHUB_MIRRORS），其他前缀与 VPS 中转在安装时用不上。
func directPosition(list []ghrelease.Source) string {
	for _, src := range list {
		if src.Kind == ghrelease.SourceDirect {
			return "first"
		}
		if ghrelease.HostMirror(src) != "" {
			break
		}
	}
	for _, src := range list {
		if src.Kind == ghrelease.SourceDirect {
			return "last"
		}
	}
	return "no"
}

// installMirrors 是一行安装命令里的 WEIR_GITHUB_MIRRORS：来源里主机名形式的镜像，按顺序；没有时为空。
func (s *Server) installMirrors(ctx context.Context) string {
	set, err := s.githubSources(ctx)
	if err != nil {
		return ""
	}
	var hosts []string
	for _, src := range set.Sources {
		if h := ghrelease.HostMirror(src); h != "" {
			hosts = append(hosts, h)
		}
	}
	return strings.Join(hosts, ",")
}

// gateBundle 打包与中心同版本的程序与本机 install.sh。不同架构的程序从 GitHub Release 取并校验。
func (s *Server) gateBundle(c *gin.Context) {
	if !s.Config.Installed {
		failure(c, 503, "setup_required")
		return
	}
	exe, _, err := s.gateProgram(c.Request.Context(), c.Query("arch"))
	if err != nil {
		failDetail(c, 409, "architecture_unavailable", err.Error(), nil)
		return
	}
	binary, err := os.ReadFile(exe)
	installer, ierr := os.ReadFile(installerPath)
	if err != nil || ierr != nil {
		failInternal(c, 503, "bundle_unavailable", errors.Join(err, ierr))
		return
	}
	files := map[string][]byte{"weir": binary, "install.sh": installer, "VERSION": []byte(buildinfo.Version + "\n")}
	var sums strings.Builder
	for _, name := range []string{"VERSION", "install.sh", "weir"} {
		h := sha256.Sum256(files[name])
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + name + "\n")
	}
	files["SHA256SUMS"] = []byte(sums.String())
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"SHA256SUMS", "VERSION", "install.sh", "weir"} {
		mode := int64(0644)
		if name == "weir" {
			mode = 0755
		}
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(files[name])), Typeflag: tar.TypeReg}); err == nil {
			_, err = tw.Write(files[name])
		}
		if err != nil {
			failInternal(c, 500, "bundle_failed", err)
			return
		}
	}
	tw.Close()
	gz.Close()
	c.Data(200, "application/gzip", buf.Bytes())
}
