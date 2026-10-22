package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/imzyb/MiGate/internal/agent"
	"github.com/imzyb/MiGate/internal/buildinfo"
)

// runGate 承接原 migate-gate：Agent、辅助程序、配对与恢复。
func runGate(args []string) int {
	command := "run"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet(command, flag.ExitOnError)
	path := f.String("config", "/etc/migate-gate/agent.json", "configuration file")
	center := f.String("center", "", "HTTPS center URL (local-pair: loopback HTTP)")
	name := f.String("name", "", "device name")
	tlsName := f.String("tls-server-name", "", "optional case variant of center hostname; certificate verification stays enabled")
	dialAddress := f.String("center-dial-address", "", "optional loopback SSH tunnel endpoint; HTTPS identity remains the center")
	tokenFile := f.String("token-file", "/etc/migate-gate/local-pair.token", "local-pair: one-time token written by the installer")
	f.Parse(args)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var err error
	switch command {
	case "pair":
		var result map[string]any
		result, err = agent.Pair(ctx, *center, *name, *path, *tlsName, *dialAddress)
		if err == nil {
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
		}
	case "local-pair":
		// 单机角色：等待本机中心初始化完成后自动配对，成功后删除一次性凭证。
		err = agent.LocalPair(ctx, *center, *name, *path, *tokenFile)
	case "helper":
		err = agent.RunHelper(ctx, "/run/migate/helper.sock")
	case "recover":
		var result map[string]any
		result, err = agent.Recover(ctx, *path)
		if err == nil {
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
		}
	case "helper-health":
		err = agent.HelperHealth(ctx)
	case "run":
		var cfg agent.Config
		cfg, err = agent.Load(*path)
		if err == nil {
			var a *agent.Agent
			a, err = agent.New(cfg)
			if err == nil {
				defer a.DB.Close()
				err = a.Run(ctx)
			}
		}
	case "version":
		fmt.Println(buildinfo.GateVersionLine())
	default:
		err = fmt.Errorf("unknown command")
	}
	if err != nil {
		return fatal(err)
	}
	return 0
}
