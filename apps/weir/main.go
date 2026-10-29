// weir 是唯一的发布程序。角色由子命令决定：
//
//	weir                 打开中心中文菜单（本机装有中心时）
//	weir center [参数]   中心服务、初始化与菜单
//	weir gate <命令>     Gate Agent、辅助程序与配对
//	weir dsh agent       dsh 管理进程（独立系统用户 weir-dsh）
//	weir self-update     以 root 从 GitHub Release 自更新（weir-update 单元调用，也可手动）
//	weir version         显示版本
package main

import (
	"fmt"
	"os"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/center"
)

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	if len(args) == 0 {
		if _, err := os.Stat(center.DefaultConfigPath); err == nil {
			return runCenter(nil)
		}
		usage()
		return 2
	}
	switch args[0] {
	case "center":
		return runCenter(args[1:])
	case "gate":
		return runGate(args[1:])
	case "kernel":
		return runKernel(args[1:])
	case "dsh":
		return runDSH(args[1:])
	case "self-update":
		return runSelfUpdate(args[1:])
	case "version", "--version":
		fmt.Println(buildinfo.ControlVersionLine())
		return 0
	case "help", "-h", "--help":
		usage()
		return 0
	}
	fmt.Fprintln(os.Stderr, "未知命令：", args[0])
	usage()
	return 2
}

func usage() {
	fmt.Println(`用法：
  weir                     打开中心中文菜单
  weir center [参数]       中心：--serve / --setup / --init / --health-check / --version
  weir gate <命令> [参数]  Gate：run / pair / local-pair / helper / helper-health / version
  weir kernel import <内核> <版本> <架构> <文件>   离线导入内核到中心缓存
  weir dsh agent --dir <目录>   dsh 管理进程（由 systemd 单元 weir-dsh 启动）
  weir self-update [--version X | --rollback | --apply]   以 root 从 GitHub Release 自更新
  weir version             显示版本`)
}

func fatal(err error) int {
	fmt.Fprintln(os.Stderr, err)
	return 1
}
