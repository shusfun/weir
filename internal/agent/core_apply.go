package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imzyb/MiGate/internal/db"
	runtimecmd "github.com/imzyb/MiGate/internal/runtime/command"
)

func managedProtocol(protocol, core string) bool {
	if core == db.CoreXray {
		return protocol == "vless" || protocol == "vmess" || protocol == "trojan"
	}
	return core == db.CoreSingbox && (protocol == "hysteria2" || protocol == "tuic")
}

type coreConfig struct {
	name     string
	body     []byte
	args     []string
	disabled bool
}

// Validate every candidate before changing either running core. If either core
// fails readiness, restore all previous configs and the previous service state.
func applyCoreConfigs(ctx context.Context, cores []coreConfig) (resultErr error) {
	dir := "/etc/migate-gate/cores"
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	runner := runtimecmd.NewRealCommandRunner(20 * time.Second)
	previous := map[string][]byte{}
	active := map[string]bool{}
	changed := []string{}
	defer func() {
		for _, core := range cores {
			os.Remove(filepath.Join(dir, core.name+".candidate.json"))
		}
		if resultErr == nil {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i := len(changed) - 1; i >= 0; i-- {
			name := changed[i]
			path := filepath.Join(dir, name+".json")
			unit := "migate-gate-" + name + ".service"
			old, exists := previous[name]
			var err error
			if exists {
				err = os.WriteFile(path, old, 0640)
			} else {
				err = os.Remove(path)
				if os.IsNotExist(err) {
					err = nil
				}
			}
			resultErr = errors.Join(resultErr, err)
			action := "stop"
			if active[name] {
				action = "restart"
			}
			_, err = runner.RunOutput(rollbackCtx, "systemctl", action, unit)
			resultErr = errors.Join(resultErr, err)
			if active[name] {
				resultErr = errors.Join(resultErr, verifyStatsReady(rollbackCtx, name))
			}
		}
	}()
	for _, core := range cores {
		candidate := filepath.Join(dir, core.name+".candidate.json")
		if err := os.WriteFile(candidate, core.body, 0640); err != nil {
			return err
		}
		if _, err := runner.RunOutput(ctx, "/opt/migate-gate/cores/"+core.name, append(core.args, candidate)...); err != nil {
			return fmt.Errorf("%s validation failed", core.name)
		}
		old, err := os.ReadFile(filepath.Join(dir, core.name+".json"))
		if err == nil {
			previous[core.name] = old
		} else if !os.IsNotExist(err) {
			return err
		}
		_, err = runner.RunOutput(ctx, "systemctl", "is-active", "--quiet", "migate-gate-"+core.name+".service")
		active[core.name] = err == nil
	}
	for _, core := range cores {
		if err := os.Rename(filepath.Join(dir, core.name+".candidate.json"), filepath.Join(dir, core.name+".json")); err != nil {
			return err
		}
		changed = append(changed, core.name)
		action := "restart"
		if core.disabled {
			action = "stop"
		}
		if _, err := runner.RunOutput(ctx, "systemctl", action, "migate-gate-"+core.name+".service"); err != nil {
			return fmt.Errorf("%s restart failed", core.name)
		}
		if core.disabled {
			continue
		}
		if err := verifyStatsReady(ctx, core.name); err != nil {
			return fmt.Errorf("%s readiness failed", core.name)
		}
	}
	return nil
}
