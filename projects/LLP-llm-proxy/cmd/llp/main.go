package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/topolik/llp-llm-proxy/internal/auth"
	"github.com/topolik/llp-llm-proxy/internal/control"
	"github.com/topolik/llp-llm-proxy/internal/registry"
	"github.com/topolik/llp-llm-proxy/internal/router"
	"github.com/topolik/llp-llm-proxy/internal/server"
	"github.com/topolik/llp-llm-proxy/internal/usage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := registry.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	reg, err := registry.Build(cfg)
	if err != nil {
		log.Fatalf("registry: %v", err)
	}

	if err := os.MkdirAll(dirOf(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("mkdir db dir: %v", err)
	}
	store, err := usage.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("usage db: %v", err)
	}
	defer store.Close()

	authStore := auth.NewStore()

	ctrlCloser, err := control.Serve(cfg.ControlSocket, authStore, cfg.RequireSameUID)
	if err != nil {
		log.Fatalf("control socket: %v", err)
	}
	defer ctrlCloser.Close()
	log.Printf("control socket %s", cfg.ControlSocket)

	rtr := router.New(reg.AllImpls())
	srv := server.New(reg, rtr, store, authStore, cfg.MaxPromptBytes, cfg.PreviewMax())

	addr := fmt.Sprintf("%s:%d", cfg.BindHost, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	log.Printf("data API on %s", addr)

	httpSrv := &http.Server{Handler: srv.Handler()}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Printf("shutting down…")
		httpSrv.Shutdown(context.Background())
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
