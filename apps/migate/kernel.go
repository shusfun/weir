package main

import (
	"flag"
	"fmt"

	"github.com/imzyb/MiGate/internal/center"
)

// runKernel：migate kernel import <内核> <版本> <架构> <文件>，把本机文件导入中心的内核缓存。
func runKernel(args []string) int {
	// 先取子命令再解析参数：flag 遇到第一个非参数就停止，--config 必须在子命令之后也能识别。
	if len(args) == 0 || args[0] != "import" {
		return fatal(fmt.Errorf("用法：migate kernel import <xray|sing-box|mihomo> <版本> <amd64|arm64> <文件>"))
	}
	f := flag.NewFlagSet("kernel import", flag.ExitOnError)
	path := f.String("config", center.DefaultConfigPath, "center configuration file")
	f.Parse(args[1:])
	rest := append([]string{"import"}, f.Args()...)
	if len(rest) != 5 {
		return fatal(fmt.Errorf("用法：migate kernel import <xray|sing-box|mihomo> <版本> <amd64|arm64> <文件>"))
	}
	cfg, err := center.LoadConfig(*path)
	if err != nil {
		return fatal(err)
	}
	sum, err := center.ImportKernel(*path, cfg, rest[1], rest[2], rest[3], rest[4])
	if err != nil {
		return fatal(err)
	}
	fmt.Printf("已导入 %s %s (%s)，SHA-256 %s\n", rest[1], rest[2], rest[3], sum)
	return 0
}
