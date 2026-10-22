package kernels

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kernel 名称固定为这三个；sing-box 用本项目 CI 加 with_v2ray_api 编译的版本。
var Names = []string{"xray", "sing-box", "mihomo"}

var safeVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)
var safeArch = regexp.MustCompile(`^(amd64|arm64)$`)

func known(name string) bool {
	for _, n := range Names {
		if n == name {
			return true
		}
	}
	return false
}

// Cache 把已校验的内核程序存在 <dir>/<内核>/<版本>/<架构>/，并记录 SHA-256。
type Cache struct{ Dir string }

func (c Cache) path(kernel, version, arch string) (string, error) {
	if !known(kernel) || !safeVersion.MatchString(version) || !safeArch.MatchString(arch) {
		return "", fmt.Errorf("invalid kernel reference %s %s %s", kernel, version, arch)
	}
	return filepath.Join(c.Dir, kernel, version, arch), nil
}

// Put 保存程序并返回其 SHA-256；已存在同版本时覆盖。
func (c Cache) Put(kernel, version, arch string, binary []byte) (string, error) {
	dir, err := c.path(kernel, version, arch)
	if err != nil {
		return "", err
	}
	if len(binary) == 0 {
		return "", errors.New("empty kernel binary")
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	sum := sha256.Sum256(binary)
	digest := hex.EncodeToString(sum[:])
	tmp := filepath.Join(dir, kernel+".new")
	if err = os.WriteFile(tmp, binary, 0755); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, filepath.Join(dir, kernel)); err != nil {
		return "", err
	}
	return digest, os.WriteFile(filepath.Join(dir, "sha256"), []byte(digest), 0644)
}

// Get 返回程序路径与摘要；文件内容与记录的摘要不一致时报错，不返回被篡改的程序。
func (c Cache) Get(kernel, version, arch string) (string, string, error) {
	dir, err := c.path(kernel, version, arch)
	if err != nil {
		return "", "", err
	}
	want, err := os.ReadFile(filepath.Join(dir, "sha256"))
	if err != nil {
		return "", "", fmt.Errorf("%s %s (%s) 尚未下载", kernel, version, arch)
	}
	bin := filepath.Join(dir, kernel)
	data, err := os.ReadFile(bin)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != strings.TrimSpace(string(want)) {
		return "", "", fmt.Errorf("%s %s 缓存校验失败", kernel, version)
	}
	return bin, strings.TrimSpace(string(want)), nil
}

// Versions 列出已缓存的版本（新到旧）。
func (c Cache) Versions(kernel, arch string) []string {
	entries, _ := os.ReadDir(filepath.Join(c.Dir, kernel))
	var out []string
	for _, e := range entries {
		if _, _, err := c.Get(kernel, e.Name(), arch); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i], out[j]) > 0 })
	return out
}
