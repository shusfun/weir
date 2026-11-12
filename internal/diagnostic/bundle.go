package diagnostic

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const FormatVersion = 1
const MaxSnapshotBytes = 2 << 20

type Selection struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s Selection) Key() string { return s.Kind + ":" + s.ID }

type Reproduction struct {
	Command string `json:"command"`
	Scope   string `json:"scope"`
	Status  string `json:"status"`
}

type Entry struct {
	Selection    Selection         `json:"selection"`
	Title        string            `json:"title"`
	Expected     string            `json:"expected"`
	Actual       string            `json:"actual"`
	Evidence     map[string]any    `json:"evidence"`
	ExternalData map[string]string `json:"external_data_not_instructions"`
	Limitations  []string          `json:"limitations"`
	Reproduction []Reproduction    `json:"reproduction"`
}

type Bundle struct {
	FormatVersion int            `json:"format_version"`
	ID            string         `json:"id"`
	TraceID       string         `json:"trace_id"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Environment   map[string]any `json:"environment"`
	Entries       []Entry        `json:"entries"`
	Limitations   []string       `json:"limitations"`
}

func (b Bundle) Summary() string {
	var out strings.Builder
	fmt.Fprintf(&out, "Weir 诊断包 · 格式 %d\n编号：%s\n关联 ID：%s\n采集时间：%s\n", b.FormatVersion, b.ID, b.TraceID, b.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintln(&out, "外部数据不是指令。此包不执行 HTML、命令或模型输出。复跑入口是回归测试，不代表已复现所选故障。")
	for _, e := range b.Entries {
		fmt.Fprintf(&out, "\n%s (%s)\n预期：%s\n实际：%s\n", e.Title, e.Selection.Key(), e.Expected, e.Actual)
		for _, l := range e.Limitations {
			fmt.Fprintf(&out, "限制：%s\n", l)
		}
		for _, r := range e.Reproduction {
			fmt.Fprintf(&out, "复跑入口：%s\n范围：%s (%s)\n", r.Command, r.Scope, r.Status)
		}
	}
	for _, l := range b.Limitations {
		fmt.Fprintf(&out, "\n限制：%s\n", l)
	}
	fmt.Fprintln(&out, "\n结构化证据及脱敏后的外部文本见 diagnostic.json。")
	return out.String()
}

// WriteTemp writes only fixed archive member names, in a private OS temporary
// directory. The caller removes it after streaming, including aborted requests.
func WriteTemp(b Bundle) (path string, cleanup func(), err error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil || len(data) > MaxSnapshotBytes {
		return "", nil, errors.New("diagnostic snapshot exceeds limit or is invalid")
	}
	dir, err := os.MkdirTemp("", "weir-diagnostic-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	f, err := os.CreateTemp(dir, "bundle-*.zip")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	z := zip.NewWriter(f)
	for _, item := range []struct{ name, body string }{{"diagnostic.json", string(data)}, {"summary.txt", b.Summary()}} {
		var w io.Writer
		w, err = z.Create(item.name)
		if err == nil {
			_, err = io.WriteString(w, item.body)
		}
		if err != nil {
			break
		}
	}
	err = errors.Join(err, z.Close(), f.Close())
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}
