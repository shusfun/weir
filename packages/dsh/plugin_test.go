package dsh

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPluginHostPolicyAndFileBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cordis.patch.yml"), []byte("url: '{{DSH_MCP_URL}}'"), 0600); err != nil {
		t.Fatal(err)
	}
	a := agent{l: Layout{Root: t.TempDir()}}
	d := Desired{PluginDir: dir, MCPURL: "http://127.0.0.1:1234/mcp", SkillsDir: t.TempDir()}
	check := func(want string) {
		t.Helper()
		base, plugin, reason, err := a.prepareWorkspace(d)
		if err != nil || len(base) != 2 || plugin != "" || !strings.Contains(reason, want) {
			t.Fatalf("unexpected degradation: %v %q %q %v", base, plugin, reason, err)
		}
		if strings.Contains(reason, "SECRET") {
			t.Fatal("validator error leaked contents")
		}
	}
	check("not configured")
	a.validatePlugin = func(files map[string]string) error { return errors.New("SECRET raw configuration") }
	check("host policy")
	a.validatePlugin = func(files map[string]string) error { return nil }
	if err := os.WriteFile(filepath.Join(dir, "script.js"), []byte("evil"), 0600); err != nil {
		t.Fatal(err)
	}
	check("files rejected")
	if err := os.Remove(filepath.Join(dir, "script.js")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), make([]byte, (64<<10)+1), 0600); err != nil {
		t.Fatal(err)
	}
	check("files rejected")
}

func TestPlaceholderValuesCannotInjectYAML(t *testing.T) {
	value := "secret'\n- insert: attacker\n\\\""
	b, err := FillPlaceholders([]byte("auth: 'Bearer {{DSH_MCP_TOKEN}}'\nskills: ['{{DSH_SKILLS_DIR}}']"), "http://127.0.0.1:9", value, value)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Auth   string
		Skills []string
	}
	if err = yaml.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Auth != "Bearer "+value || len(got.Skills) != 1 || got.Skills[0] != value {
		t.Fatalf("values changed: %#v", got)
	}
	if _, err = FillPlaceholders([]byte("a: b\n---\nx: y"), "", "", ""); err == nil {
		t.Fatal("multiple documents accepted")
	}
}

func TestMCPEndpointRejectsCredentialsAndRemoteTargets(t *testing.T) {
	for _, raw := range []string{"http://example.com:80/mcp", "http://localhost:80/mcp", "http://127.0.0.1:80/mcp?secret=SECRET", "http://user:SECRET@127.0.0.1:80/mcp", "file:///SECRET", "http://127.0.0.1:80/#SECRET"} {
		if _, err := loopbackEndpoint(raw); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("unsafe endpoint/error: %q %v", raw, err)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:9/mcp", "http://[::1]:9/mcp"} {
		if _, err := loopbackEndpoint(raw); err != nil {
			t.Fatal(err)
		}
	}
}
