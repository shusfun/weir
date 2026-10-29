package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shusfun/weir/internal/selfupdate"
)

// runSelfUpdate：--apply 执行中心写入的请求（weir-update.service 调用）；
// --version X / --rollback 由 root 在终端直接发起，进度同样写进 status.json，面板里能看到。
func runSelfUpdate(args []string) int {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "执行中心写入的更新请求")
	version := fs.String("version", "", "更新到指定版本（不带 v）")
	rollback := fs.Bool("rollback", false, "切回本机保留的上一版本")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if os.Geteuid() != 0 {
		return fatal(fmt.Errorf("需要 root"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if !*apply {
		var rnd [8]byte
		_, _ = rand.Read(rnd[:])
		req := selfupdate.Request{ID: "cli-" + hex.EncodeToString(rnd[:]), Action: "update", Version: *version}
		if *rollback {
			req = selfupdate.Request{ID: req.ID, Action: "rollback"}
		} else if *version == "" {
			fs.Usage()
			return 2
		}
		if err := selfupdate.WriteRequest(req); err != nil {
			return fatal(err)
		}
	}
	if err := selfupdate.Apply(ctx); err != nil {
		return fatal(err)
	}
	if st, ok := selfupdate.ReadStatus(); ok {
		fmt.Printf("%s：%s → %s，%s\n", st.Action, st.From, st.Version, st.State)
	}
	return 0
}
