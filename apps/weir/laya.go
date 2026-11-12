package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/shusfun/weir/internal/layahost"
)

func runLaya(args []string) int {
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
