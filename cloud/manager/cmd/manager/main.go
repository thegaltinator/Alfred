package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"alfred-cloud/manager"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtime, err := manager.NewRuntimeFromEnv(ctx)
	if err != nil {
		log.Fatalf("manager bootstrap failed: %v", err)
	}

	srv := &http.Server{
		Addr:         runtime.ListenAddr(),
		Handler:      runtime.Handler(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		if err := runtime.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("manager runtime stopped: %v", err)
			stop()
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("manager service listening on %s (ctrl+c to stop)", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("manager http server error: %v", err)
	}
	log.Println("manager service exited")
}
