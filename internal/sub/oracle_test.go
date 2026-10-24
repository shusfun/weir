package sub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	js "github.com/shusfun/weir/internal/sub/jscompat"
)

// oracleCase 描述一次“同一输入分别交给上游 JS 与 Go 移植执行”的对照。
type oracleCase struct {
	Name       string
	Mod        string // 相对 testdata/upstream/functions 的模块路径
	Fn         string // 导出名
	Prop       string // 导出对象上的属性（可选）
	Args       []any  // jscompat 值，经 Describe 编码后交给 Node
	ReturnArgs bool   // 同时比较调用后的参数（用于观察就地修改）
	Go         func() (any, error)
}

type oracleWire struct {
	Mod        string          `json:"mod"`
	Fn         string          `json:"fn"`
	Prop       string          `json:"prop,omitempty"`
	Args       json.RawMessage `json:"args"`
	ReturnArgs bool            `json:"returnArgs,omitempty"`
}

func runOracle(t *testing.T, cases []oracleCase) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未找到 node，跳过与上游 MiSub JS 的对照测试")
	}
	if _, err := os.Stat(filepath.Join("testdata", "upstream", "node_modules", "js-yaml")); err != nil {
		t.Skip("缺少对照依赖：在 internal/sub/testdata/upstream 执行 npm install --no-save js-yaml@4.1.1")
	}
	wire := make([]oracleWire, len(cases))
	for i, c := range cases {
		wire[i] = oracleWire{Mod: c.Mod, Fn: c.Fn, Prop: c.Prop, ReturnArgs: c.ReturnArgs,
			Args: json.RawMessage(js.Describe(&js.Array{Items: c.Args}))}
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join("testdata", "upstream", "oracle.mjs"), file)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("运行上游对照脚本失败: %v\n%s", err, stderr.String())
	}
	var expected []string
	if err := json.Unmarshal(out, &expected); err != nil {
		t.Fatalf("解析对照输出失败: %v\n%s", err, out)
	}
	t.Logf("与上游对照 %d 个用例", len(cases))
	if len(expected) != len(cases) {
		t.Fatalf("对照结果数量不一致: %d != %d", len(expected), len(cases))
	}
	for i, c := range cases {
		got, gerr := c.Go()
		want := expected[i]
		if strings.HasPrefix(want, `{"$throw":`) {
			if gerr == nil {
				t.Errorf("[%s] 上游抛出异常 %s，Go 未返回错误，结果: %s", c.Name, want, js.Describe(got))
			}
			continue
		}
		if gerr != nil {
			t.Errorf("[%s] Go 返回错误 %v，上游结果: %s", c.Name, gerr, want)
			continue
		}
		if g := js.Describe(got); g != want {
			t.Errorf("[%s] 与上游不一致\n%s", c.Name, diffSnippet(want, g))
		}
	}
}

// diffSnippet 只展示第一处差异附近的内容，避免长配置淹没输出。
func diffSnippet(want, got string) string {
	if len(want) < 400 && len(got) < 400 {
		return "  上游: " + want + "\n  Go  : " + got
	}
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	from := max(0, i-120)
	cut := func(s string) string {
		end := min(len(s), i+200)
		if from >= len(s) {
			return ""
		}
		return s[from:end]
	}
	return "  第 " + js.NumberToString(float64(i)) + " 字节起不同\n  上游: …" + cut(want) + "\n  Go  : …" + cut(got)
}

// ok 包装无错误的返回值。
func ok(v any) (any, error) { return v, nil }

// strs 把字符串切片转换为 jscompat 数组。
func strs(items ...string) *js.Array { return js.StrArr(items) }
