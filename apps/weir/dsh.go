package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/shusfun/weir/internal/dsh"
)

// runDSH：migate dsh agent --dir <托管目录>。dsh 的管理进程，以系统用户 migate-dsh 运行
// （生产环境由 systemd 单元 migate-dsh 启动；开发容器里由中心降权启动）。它读不到中心的配置与数据库，
// 只按 control/ 里的期望状态运行 dsh，并把状态写到 status/。
func runDSH(args []string) int {
	if len(args) == 0 || args[0] != "agent" {
		return fatal(fmt.Errorf("用法：migate dsh agent --dir <托管目录>"))
	}
	f := flag.NewFlagSet("dsh agent", flag.ExitOnError)
	dir := f.String("dir", "/var/lib/migate/dsh", "dsh hosting directory shared with the center")
	f.Parse(args[1:])
	if f.NArg() > 0 {
		return fatal(fmt.Errorf("用法：migate dsh agent --dir <托管目录>"))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := dsh.RunAgent(ctx, *dir); err != nil {
		return fatal(err)
	}
	return 0
}
