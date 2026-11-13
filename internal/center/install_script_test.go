package center

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateInstallPairingFailureHandling(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("shell script execution requires sh")
	}
	for _, outcome := range []string{"success", "rejected", "local-failure"} {
		t.Run(outcome, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, body string) string {
				path := filepath.Join(root, name)
				if err := os.WriteFile(path, []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
				return path
			}
			log := filepath.Join(root, "calls")
			config := write("agent.json", "old identity")
			write("systemctl", "#!/bin/sh\necho \"$*\" >> \"$CALLS\"\n")
			write("install.sh", "#!/bin/sh\n[ \"$WEIR_GATE_PAIRING\" = 1 ] || exit 9\necho install >> \"$CALLS\"\n")
			pair := write("weir", `#!/bin/sh
echo "$*" >> "$CALLS"
case "$OUTCOME" in
 success) rm -f "$CONFIG.pairing"; exit 0;;
 rejected) echo 'pair rejected: center HTTP 403 code=pair_token_invalid request=abc123'; exit 1;;
 local-failure) touch "$CONFIG.pairing"; echo 'state cleanup failed'; exit 1;;
esac
`)
			start := strings.Index(gateScript, "WEIR_GATE_PAIRING=1 sh")
			if start < 0 {
				t.Fatal("installer does not suppress the old agent during re-pair")
			}
			script := strings.NewReplacer("/opt/weir/current/weir", `"$PAIR"`, "/etc/weir/gate/agent.json", `"$CONFIG"`, "%[1]s", "https://center.example.test").Replace(gateScript[start:])
			path := write("run.sh", "#!/bin/sh\nset -eu\nlaya=no\n"+script)
			cmd := exec.Command(sh, path)
			cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "work="+root, "token=test-invite", "CALLS="+log, "CONFIG="+config, "PAIR="+pair, "OUTCOME="+outcome)
			out, err := cmd.CombinedOutput()
			calls, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if (err == nil) != (outcome == "success") || strings.Contains(string(out), "Gate paired;") != (outcome == "success") {
				t.Fatalf("outcome=%s err=%v output=%s", outcome, err, out)
			}
			lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
			if len(lines) < 3 || lines[0] != "install" || lines[1] != "stop weir-gate.service" || !strings.Contains(lines[2], "gate pair --replace") {
				t.Fatalf("wrong re-pair ordering: %s", calls)
			}
			if strings.Contains(string(calls), "restart weir-gate.service") != (outcome != "local-failure") {
				t.Fatalf("unsafe service restart: %s", calls)
			}
			if outcome == "rejected" && !strings.Contains(string(out), "request=abc123") {
				t.Fatalf("installer lost the pairing cause: %s", out)
			}
		})
	}
}
