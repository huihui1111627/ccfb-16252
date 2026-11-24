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

	"airport-baggage/internal/api"
	"airport-baggage/internal/sim"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	seed := flag.Int64("seed", 16252, "deterministic simulation seed")
	flag.Parse()

	engine := sim.NewEngine(*seed)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go engine.Run(ctx)

	server := &http.Server{
		Addr:              *addr,
		Handler:           api.NewServer(engine).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("机场行李中间件模拟已启动：http://localhost%s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
