package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/internal/center"
	"github.com/shusfun/weir/internal/gate"
)

//go:embed static/*
var embedded embed.FS

// runCenter：中心服务、初始化与中文菜单。
func runCenter(args []string) int {
	f := flag.NewFlagSet("center", flag.ExitOnError)
	path := f.String("config", center.DefaultConfigPath, "configuration file")
	listen := f.String("listen", "", "listen address override")
	initToken := f.Bool("init", false, "create a short-lived initialization token")
	showToken := f.Bool("token", false, "print a usable initialization token, rotating it if expired")
	health := f.Bool("health-check", false, "check the local HTTP service and its store")
	version := f.Bool("version", false, "print release version")
	setupCLI := f.Bool("setup", false, "run the setup wizard in the terminal")
	serve := f.Bool("serve", false, "run the HTTP service without opening the menu")
	localGate := f.String("local-gate", os.Getenv("WEIR_LOCAL_GATE"), "standalone: run the local Gate agent in this process once this device file exists")
	f.Parse(args)
	if f.NArg() > 0 {
		return fatal(fmt.Errorf("不支持的参数。运行 weir help 查看用法"))
	}
	if *version {
		fmt.Println(buildinfo.ControlVersionLine())
		return 0
	}
	if *setupCLI {
		if err := center.RunSetupCLI(*path, os.Stdin, os.Stdout, center.ReadSecret); err != nil {
			return fatal(err)
		}
		return 0
	}
	cfg, err := center.LoadConfig(*path)
	if err != nil {
		return fatal(err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *health {
		return healthCheck(cfg)
	}
	if *initToken {
		token, err := center.RotateSetupToken(*path, cfg)
		if err != nil {
			return fatal(err)
		}
		fmt.Println("Initialization token (valid 30 minutes):", token)
		return 0
	}
	if *showToken {
		token, left, rotated, err := center.CurrentSetupToken(*path, cfg)
		if err != nil {
			return fatal(err)
		}
		center.PrintSetupToken(os.Stdout, center.DescribeSetupToken(left, rotated), token)
		return 0
	}
	if !*serve && *listen == "" {
		switch center.SelectCLIMode() {
		case "menu":
			if err := center.RunOperatorMenu(*path, os.Stdin, os.Stdout); err != nil {
				return fatal(err)
			}
			return 0
		case "no-terminal":
			fmt.Fprintln(os.Stderr, "没有交互终端。运行 weir 打开菜单，或使用 weir center --serve 在后台启动。")
			return 2
		}
	}
	if _, statErr := os.Stat(*path); os.IsNotExist(statErr) {
		if _, err = center.Initialize(*path, cfg); err != nil {
			return fatal(err)
		}
		if cfg, err = center.LoadConfig(*path); err != nil {
			return fatal(err)
		}
	}
	assets, _ := fs.Sub(embedded, "static")
	server, err := center.New(cfg, *path, assets)
	if err != nil {
		return fatal(err)
	}
	defer server.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server.StartBackground(ctx)
	if *localGate != "" {
		go runLocalGate(ctx, *localGate)
	}
	httpServer := &http.Server{Addr: cfg.Listen, Handler: server.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 65 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		httpServer.Shutdown(shutdown)
	}()
	fmt.Println("Weir center listening on", cfg.Listen)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fatal(err)
	}
	return 0
}

// runLocalGate 是单机角色的本机 Gate：与中心在同一进程里运行，走同一套同步协议（经回环连接）。
// 设备文件由安装器的本机配对写出；还没有时每 5 秒检查一次。内核仍由 root 辅助程序托管。
func runLocalGate(ctx context.Context, config string) {
	for {
		if _, err := os.Stat(config); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
	p := gate.DefaultPaths()
	p.Config = config
	for ctx.Err() == nil {
		a, err := gate.NewAgent(p)
		if err == nil {
			err = a.Run(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		log.Printf("local gate: %v; restarting in 10s", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func healthCheck(cfg center.Config) int {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return fatal(err)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return fatal(fmt.Errorf("health check unavailable"))
	}
	defer resp.Body.Close()
	var result struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil || resp.StatusCode != 200 || cfg.Installed && result.Status != "ok" {
		return fatal(fmt.Errorf("health check failed"))
	}
	fmt.Println(result.Status)
	return 0
}
