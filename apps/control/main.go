package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/imzyb/MiGate/internal/buildinfo"
	"github.com/imzyb/MiGate/internal/control"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed static/*
var embedded embed.FS

func main() {
	f := flag.NewFlagSet("control", flag.ExitOnError)
	path := f.String("config", "/etc/migate-control/config.json", "configuration file")
	listen := f.String("listen", "", "listen address override")
	public := f.String("public-url", "", "public HTTPS URL")
	init := f.Bool("init", false, "create setup configuration and print short-lived initialization token")
	health := f.Bool("health-check", false, "check configured local HTTP and dependencies")
	version := f.Bool("version", false, "print release version")
	setupCLI := f.Bool("setup", false, "run the setup wizard in the terminal")
	f.Parse(os.Args[1:])
	if *version {
		fmt.Println(buildinfo.ControlVersionLine())
		return
	}
	if *setupCLI {
		if err := control.RunSetupCLI(*path, os.Stdin, control.ReadSecret); err != nil {
			fatal(err)
		}
		return
	}
	cfg, err := control.LoadConfig(*path)
	if err != nil {
		fatal(err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *public != "" {
		cfg.PublicURL = *public
	}
	if *health {
		host, port, e := net.SplitHostPort(cfg.Listen)
		if e != nil {
			fatal(e)
		}
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		client := http.Client{Timeout: 3 * time.Second}
		resp, e := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
		if e != nil {
			fatal(fmt.Errorf("health check unavailable"))
		}
		defer resp.Body.Close()
		var result struct {
			Status string `json:"status"`
		}
		if json.NewDecoder(resp.Body).Decode(&result) != nil || resp.StatusCode != 200 || cfg.Installed && result.Status != "ok" {
			fatal(fmt.Errorf("health check failed"))
		}
		fmt.Println(result.Status)
		return
	}
	if *init {
		token, err := control.Initialize(*path, cfg)
		if err != nil {
			fatal(err)
		}
		fmt.Println("Initialization token (valid 30 minutes):", token)
		return
	}
	if _, statErr := os.Stat(*path); os.IsNotExist(statErr) {
		token, err := control.Initialize(*path, cfg)
		if err != nil {
			fatal(err)
		}
		fmt.Println("Initialization token (valid 30 minutes):", token)
		cfg, err = control.LoadConfig(*path)
		if err != nil {
			fatal(err)
		}
	}
	assets, _ := fs.Sub(embedded, "static")
	server, err := control.New(cfg, *path, assets)
	if err != nil {
		fatal(err)
	}
	defer server.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if cfg.Installed {
		go server.Workers(ctx)
	}
	httpServer := &http.Server{Addr: cfg.Listen, Handler: server.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 65 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		httpServer.Shutdown(shutdown)
	}()
	fmt.Println("MiGate control listening on", cfg.Listen)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

var _ = filepath.Join
