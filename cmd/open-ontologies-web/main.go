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
	"syscall"
	"time"
)

var revision = "unknown"
var modified = "true"

func version() string {
	r := revision
	if len(r) > 12 {
		r = r[:12]
	}
	v := "0.1.0+local." + r
	if modified != "false" {
		v += ".dirty"
	}
	return v
}
func run() error {
	addr := flag.String("listen", "127.0.0.1:18085", "loopback listener")
	path := flag.String("config", "", "public service JSON config")
	show := flag.Bool("version", false, "show build identity")
	flag.Parse()
	if *show {
		fmt.Println("open-ontologies-web " + version())
		return nil
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("listen must be loopback")
	}
	f, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cfg adapter.PublicConfig
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return err
	}
	app, err := adapter.NewPublicWeb(cfg, version())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	err = app.Ready(ctx)
	cancel()
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("public read-only web service ready", "version", version(), "graphs", len(cfg.Graphs), "max_workers", 2)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func main() {
	if err := run(); err != nil {
		slog.Error("public web service stopped", "error", err)
		os.Exit(1)
	}
}
