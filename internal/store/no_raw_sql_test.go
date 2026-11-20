package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// sqlLiteral 匹配 SQL 语句片段。仓库里的 SQL 一律大写关键字，按大小写敏感匹配，避免误报英文提示文案。
var sqlLiteral = regexp.MustCompile(`\b(SELECT\b[\s\S]+\bFROM\b|INSERT INTO\b|DELETE FROM\b|UPDATE \S+ SET\b|CREATE (UNIQUE )?(TABLE|INDEX|TRIGGER|SCHEMA|FUNCTION|OR REPLACE)\b|ALTER TABLE\b|DROP (TABLE|INDEX|TRIGGER)\b|PRAGMA |pg_advisory)`)

// rawSQLMethods 是 database/sql 执行语句的方法名；Ent 的构造器不使用这些名字。
var rawSQLMethods = map[string]bool{"ExecContext": true, "QueryContext": true, "QueryRowContext": true, "PrepareContext": true}

// rawSQLAllowed 列出 store 之外确需直接读写数据库的文件及原因。
var rawSQLAllowed = map[string]string{
	// Gate 只读打开宿主机上 1Panel 的 SQLite 取证书管理凭据，这是第三方面板的数据库，不属于 Weir 的存储。
	"gate/panel_discovery.go": "third-party panel database",
}

// TestNoRawSQLOutsideStore 保证数据访问只经 internal/store：手写 SQL 与 database/sql 调用只能出现在
// internal/store/raw_*.go，其余代码通过 Ent 或 store 提供的方法访问数据。测试文件与 Ent 生成代码不在检查范围。
func TestNoRawSQLOutsideStore(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if strings.HasPrefix(rel, "store/raw_") && !strings.Contains(strings.TrimPrefix(rel, "store/"), "/") {
			return nil
		}
		if _, ok := rawSQLAllowed[rel]; ok {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		// Ent 生成的代码自带 SQL 构造，手写在 ent 包里的文件（如 schema）照常检查。
		if ast.IsGenerated(file) {
			return nil
		}
		checked++
		sqlImport := ""
		for _, spec := range file.Imports {
			if p, _ := strconv.Unquote(spec.Path.Value); p == "database/sql" {
				sqlImport = "sql"
				if spec.Name != nil {
					sqlImport = spec.Name.Name
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				if text, err := strconv.Unquote(n.Value); err == nil && sqlLiteral.MatchString(text) {
					t.Errorf("%s: SQL literal outside internal/store/raw_*.go: %.60q", fset.Position(n.Pos()), text)
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && sqlImport != "" && pkg.Name == sqlImport && sel.Sel.Name == "Open" {
					t.Errorf("%s: database/sql connection outside internal/store/raw_*.go", fset.Position(n.Pos()))
				}
				if rawSQLMethods[sel.Sel.Name] {
					t.Errorf("%s: database/sql %s outside internal/store/raw_*.go", fset.Position(n.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no source files scanned")
	}
}
