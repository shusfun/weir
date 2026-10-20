package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/imzyb/MiGate/internal/agent"
	"github.com/imzyb/MiGate/internal/buildinfo"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	command := "run"
	args := os.Args[1:]
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet(command, flag.ExitOnError)
	path := f.String("config", "/etc/migate-gate/agent.json", "configuration file")
	center := f.String("center", "", "HTTPS center URL")
	name := f.String("name", "", "device name")
	tlsName := f.String("tls-server-name", "", "optional case variant of center hostname; certificate verification stays enabled")
	dialAddress := f.String("center-dial-address", "", "optional loopback SSH tunnel endpoint; HTTPS identity remains the center")
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
