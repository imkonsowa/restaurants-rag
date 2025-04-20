package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/imkonsowa/restaurants-rag/config"
	"github.com/nats-io/nats.go"
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

	worker := errgroup.Group{}
	errChan := make(chan error)

	for subject, h := range subjectHandlers {
		worker.Go(func() error {
			return nc.Subscribe(ctx, subject, func(m *nats.Msg) {
				if err := h(ctx, m.Data); err != nil {
					slog.Error("Failed to handle CDC message", "err", err, "subject", subject)

					if err := m.Nak(); err != nil {
						slog.Error("Failed to nak message", "err", err)
					}

					return
				}

				if err := m.Ack(); err != nil {
					slog.Error("Failed to ack message", "err", err)
				}
			})
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
