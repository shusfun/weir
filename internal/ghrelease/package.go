package ghrelease

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// VerifiedPackage 解开 Weir 发布包，按包内 SHA256SUMS 校验每个文件，返回 weir 与 install.sh 的内容。
// 中心缓存其他架构的 Gate 程序、Gate 自己从 GitHub 更新都用它。
func VerifiedPackage(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || strings.Contains(name, "..") || strings.Contains(name, "/") {
			continue
		}
		if name == "weir" || name == "install.sh" || name == "SHA256SUMS" || name == "VERSION" {
			if files[name], err = io.ReadAll(io.LimitReader(tr, 256<<20)); err != nil {
				return nil, err
			}
		}
	}
	want := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(files["SHA256SUMS"]))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			want[f[1]] = f[0]
		}
	}
	for _, name := range []string{"weir", "install.sh"} {
		got := sha256.Sum256(files[name])
		if len(files[name]) == 0 || want[name] != hex.EncodeToString(got[:]) {
			return nil, fmt.Errorf("发布包里的 %s 与 SHA256SUMS 不一致", name)
		}
	}
	return files, nil
}
