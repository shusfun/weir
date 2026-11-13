package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"github.com/shusfun/weir/internal/gate"
	"os"
	"os/signal"
	"syscall"

	"github.com/shusfun/weir/internal/layahost"
	"github.com/shusfun/weir/internal/releasefiles"
)

func runLaya(args []string) int {

	if len(args) >= 2 && args[0] == "prepare" {
		flags := flag.NewFlagSet("laya prepare", flag.ContinueOnError)
		id := flags.Int64("gate-id", 0, "existing Gate ID")
		pub := flags.String("public-key", "", "expected public key")
		center := flags.String("center", "", "expected center")
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
			return 2
		}
		if *id != 0 || *pub != "" || *center != "" {
			if args[1] != "gate" || *id <= 0 || *pub == "" || *center == "" {
				return fatal(errors.New("gate_identity_required"))
			}
			cfg, err := gate.LoadConfig("/etc/weir/gate/agent.json")
			if err != nil {
				return fatal(errors.New("gate_identity_unavailable"))
			}
			key, e := base64.StdEncoding.DecodeString(cfg.PrivateKey)
			if e != nil || len(key) != ed25519.PrivateKeySize || cfg.GateID != *id || cfg.Center != *center || base64.StdEncoding.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey)) != *pub {
				return fatal(errors.New("gate_identity_conflict"))
			}
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		ctx = releasefiles.WithTrace(ctx, os.Getenv("WEIR_TRACEPARENT"))
		if _, err := layahost.Prepare(ctx, args[1], func(stage string) {
			fmt.Printf("laya.prepare stage=%s trace=%s\n", stage, releasefiles.Trace(ctx))
		}); err != nil {
			return fatal(err)
		}
		return 0
	}

	if len(args) == 0 || args[0] != "helper" {
		fmt.Fprintln(os.Stderr, "用法：weir laya helper [--directory /opt/weir/center-laya] [--socket /run/weir-laya/helper.sock] [--group weir-center]")
		return 2
	}
	flags := flag.NewFlagSet("laya helper", flag.ContinueOnError)
	directory := flags.String("directory", "/opt/weir/center-laya", "root-owned runtime approval directory")
	socket := flags.String("socket", "/run/weir-laya/helper.sock", "dedicated Unix socket")
	group := flags.String("group", "weir-center", "authorized caller group")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := layahost.Serve(ctx, *directory, *socket, *group); err != nil {
		return fatal(err)
	}
	return 0
}
