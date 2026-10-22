package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/gate"
)

// runGate：Gate 执行端。run 为 Agent（migate-gate 身份），helper 为 root 辅助程序。
func runGate(args []string) int {
	command := "run"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	p := gate.DefaultPaths()
	f := flag.NewFlagSet("gate "+command, flag.ExitOnError)
	f.StringVar(&p.Config, "config", p.Config, "device configuration file")
	f.StringVar(&p.Data, "data", p.Data, "agent state directory")
	f.StringVar(&p.Socket, "socket", p.Socket, "helper socket")
	center := f.String("center", "", "center origin, e.g. https://sub.example.com")
	token := f.String("token", "", "one-time pairing token")
	tokenFile := f.String("token-file", "/etc/migate/gate/local-pair.token", "local-pair: one-time token written by the installer")
	name := f.String("name", "", "device name (default: hostname)")
	caFile := f.String("ca-file", "", "optional CA certificate for a center with a private certificate")
	f.Parse(args)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var err error
	switch command {
	case "run":
		var a *gate.Agent
		if a, err = gate.NewAgent(p); err == nil {
			err = a.Run(ctx)
			if ctx.Err() != nil {
				err = nil
			}
		}
	case "helper":
		err = gate.ServeHelper(ctx, gate.NewHelper(p))
	case "pair":
		err = gate.Pair(ctx, p, *center, *token, *name, *caFile)
	case "local-pair":
		err = gate.LocalPair(ctx, p, *center, *tokenFile)
	case "helper-health":
		err = gate.HelperHealth(ctx, p)
	case "version":
		fmt.Println(buildinfo.GateVersionLine())
	default:
		err = fmt.Errorf("unknown gate command %q", command)
	}
	if err != nil {
		return fatal(err)
	}
	return 0
}
