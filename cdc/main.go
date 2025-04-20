package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/imkonsowa/restaurants-rag/config"
)

func main() {
	cfg := config.LoadConfig()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	errChan := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nc, err := NewNatsClient(&cfg.Nats)
	if err != nil {
		log.Fatal(nc)
	}
	defer nc.Close()

	listener := NewListener(cfg, nc)
	defer listener.Close(ctx)

	go func() {
		errChan <- listener.Run(ctx)
	}()

	select {
	case err := <-errChan:
		log.Fatalln("Error:", err)
	case <-shutdown:
		log.Println("Shutting down...")
		cancel()

		// wait until listener.Run returns
		<-errChan
	}
}
