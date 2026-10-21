package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/originchaindb/agent-action-inbox/internal/inbox"
)

func main() {
	envFile := flag.String("env", ".env", "optional environment file (existing environment wins)")
	flag.Parse()
	if err := inbox.LoadEnv(*envFile); err != nil {
		log.Fatal(err)
	}
	config, err := inbox.ReadConfig()
	if err != nil {
		log.Fatal(err)
	}
	database := inbox.NewCloud(config)
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	err = database.Init(ctx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	service := inbox.NewService(config, database, inbox.NewProvider(config), inbox.NewGitHub(config))
	server := &http.Server{Addr: config.Addr, Handler: inbox.Handler(config, service), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Agent Action Inbox listening on %s; provider=%s; execution_enabled=%t", config.Addr, config.Provider, config.Execute)
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.ListenAndServe() }()
	select {
	case err := <-serveResult:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case <-signals.Done():
		// Keep main alive while in-flight execution and persistence finish.
		ctx, cancel := context.WithTimeout(context.Background(), 140*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Print("Graceful shutdown timed out; inspect executing actions after restart.")
		}
	}
}
