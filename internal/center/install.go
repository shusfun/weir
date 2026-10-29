package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shusfun/weir/internal/buildinfo"
)

// installerPath 是安装时保存在本机的 install.sh；Gate 安装包直接使用它，保证与中心同版本。
const installerPath = "/usr/local/lib/weir/install.sh"

// gateScript 是「添加 Gate」生成的一行命令所执行的脚本。凭证只作为参数传入，不放进 URL。
const gateScript = `#!/bin/sh
# Weir Gate 一键安装：下载与中心同版本的程序，安装 Gate 角色，并用一次性凭证配对。
set -eu
token=''
while [ $# -gt 0 ]; do
 case "$1" in --token) token=$2; shift 2;; *) shift;; esac
done
[ -n "$token" ] || { echo 'usage: sh gate.sh --token TOKEN' >&2; exit 2; }
[ "$(id -u)" = 0 ] || { echo 'root required' >&2; exit 1; }
case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo 'unsupported architecture' >&2; exit 1;; esac
work=$(mktemp -d /tmp/weir-gate-XXXXXX)
trap 'rm -rf "$work"' EXIT
curl -fsSL "%[1]s/install/gate.tar.gz?arch=$arch" -o "$work/gate.tar.gz"
tar -xzf "$work/gate.tar.gz" -C "$work"
sh "$work/install.sh" gate
/opt/weir/current/weir gate pair --center '%[1]s' --token "$token" --config /etc/weir/gate/agent.json
systemctl enable --now weir-gate.service
echo 'Gate paired; it will appear online in the center within a minute.'
`

func (s *Server) installRoutes(r *gin.Engine) {
	r.GET("/install/gate.sh", func(c *gin.Context) {
		if !s.Config.Installed {
			failure(c, 503, "setup_required")
			return
		}
		origin := s.siteOrigin(c)
		c.Data(200, "text/x-shellscript; charset=utf-8", []byte(fmt.Sprintf(gateScript, origin)))
	})
	r.GET("/install/gate.tar.gz", s.gateBundle)
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
		failure(c, 503, "bundle_unavailable")
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
			failure(c, 500, "bundle_failed")
			return
		}
	}
	tw.Close()
	gz.Close()
	c.Data(200, "application/gzip", buf.Bytes())
}
