// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"open-ontologies-mcp-adapter/internal/adapter"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var revision = "unknown"
var modified = "true"

func version() string {
	v := "0.2.0+local." + revision
	if len(revision) > 12 {
		v = "0.2.0+local." + revision[:12]
	}
	if modified != "false" {
		v += ".dirty"
	}
	return v
}
func main() {
	if err := run(); err != nil {
		slog.Error("adapter stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	listen := flag.String("listen", "127.0.0.1:18084", "Loopback HTTP listener")
	config := flag.String("config", "", "JSON configuration file")
	showVersion := flag.Bool("version", false, "Print adapter build identity")
	flag.Parse()
	if *showVersion {
		fmt.Println("open-ontologies-mcp-adapter " + version())
		return nil
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("listen must use a loopback IP")
	}
	f, err := os.Open(*config)
	if err != nil {
		return err
	}
	defer f.Close()
	var cfg adapter.Config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.BackendURL) == "" {
		return fmt.Errorf("backend_url is required")
	}
	a, err := adapter.New(cfg, version())
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(c)
	}()
	slog.Info("Open Ontologies interface adapter ready", "version", version(), "listen", *listen)
	if err = server.ListenAndServe(); err == http.ErrServerClosed {
		return nil
	}
	return err
}
