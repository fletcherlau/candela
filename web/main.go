package main

import (
	"candela/web/internal/server"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	issuer := os.Getenv("CF_ACCESS_ISSUER")
	origin := os.Getenv("WEB_ORIGIN")
	if !strings.HasPrefix(issuer, "https://") || !strings.HasPrefix(origin, "https://") {
		log.Fatal("CF_ACCESS_ISSUER and WEB_ORIGIN must use HTTPS")
	}
	staticDir := os.Getenv("WEB_STATIC_DIR")
	if staticDir == "" {
		staticDir = "frontend/dist"
	}
	cfg := server.Config{Issuer: issuer, Audience: os.Getenv("CF_ACCESS_AUD"), Origin: origin, SyncerURL: os.Getenv("SYNCER_API_BASE"), APIKey: os.Getenv("SYNC_API_KEY"), Static: os.DirFS(staticDir)}
	if dir := os.Getenv("WEB_RESEARCH_DIR"); dir != "" {
		cfg.Research = os.DirFS(dir)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app, err := server.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	addr := os.Getenv("WEB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("Candela website listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
