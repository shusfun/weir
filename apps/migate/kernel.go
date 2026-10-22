package main

import (
	"flag"
	"fmt"

	"github.com/imzyb/MiGate/internal/center"
)

// runKernel：migate kernel import <内核> <版本> <架构> <文件>，把本机文件导入中心的内核缓存。
func runKernel(args []string) int {
	f := flag.NewFlagSet("kernel", flag.ExitOnError)
	path := f.String("config", center.DefaultConfigPath, "center configuration file")
	f.Parse(args)
	rest := f.Args()
	if len(rest) != 5 || rest[0] != "import" {
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
