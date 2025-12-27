package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/imkonsowa/restaurants-rag/config"
	"github.com/tmc/langchaingo/llms/ollama"
	"golang.org/x/sync/errgroup"
)

func main() {
	cfg := config.LoadConfig()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nc, err := NewNats(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	// Create a new Ollama instance of the embedding model
	llm, err := ollama.New(
		ollama.WithServerURL(cfg.Ollama.Address()),
		ollama.WithModel(cfg.Ollama.EmbeddingModel),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create a new Postgres instance
	pg, err := NewPg(cfg.Postgres.ConnStr())
	if err != nil {
		log.Fatal(err)
	}

	// Create a new handler
	handler, err := NewHandler(llm, pg)
	if err != nil {
		log.Fatal(err)
	}

	subjectHandlers := map[string]func(ctx context.Context, msg []byte) error{
		cfg.Nats.RestaurantsSubject: handler.HandleRestaurantCDCMessage,
		cfg.Nats.MenuItemsSubject:   handler.HandleMenuItemCDCMessage,
		cfg.Nats.CategoriesSubject:  handler.HandleCategoryCDCMessage,
	}

	workers := cfg.Embedder.Workers
	if workers < 1 {
		workers = 2
	}
	queueSize := cfg.Embedder.QueueSize
	if queueSize < 1 {
		queueSize = 100
	}
	slog.Info("Starting embedder", "workers", workers, "queueSize", queueSize)

	workerPools := make(map[string]*WorkerPool)
	for subject, h := range subjectHandlers {
		workerPools[subject] = NewWorkerPool(ctx, workers, queueSize, h)
	}

	worker := errgroup.Group{}
	errChan := make(chan error)

	for subject, pool := range workerPools {
		worker.Go(func() error {
			return nc.Subscribe(ctx, subject, pool)
		})
	}

	go func() {
		errChan <- worker.Wait()
	}()

	// Wait for a signal to shutdown
	select {
	case <-shutdown:
		slog.Info("Shutting down")
		cancel()
	case <-errChan:
		slog.Info("Shutting down due to error", "error", err)
		cancel()

	}
}
