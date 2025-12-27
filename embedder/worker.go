package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go"
)

type WorkerPool struct {
	jobs    chan *nats.Msg
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	handler func(ctx context.Context, msg []byte) error
}

func NewWorkerPool(ctx context.Context, maxWorkers, queueSize int, handler func(ctx context.Context, msg []byte) error) *WorkerPool {
	if maxWorkers < 1 {
		maxWorkers = 2
	}
	if queueSize < 1 {
		queueSize = 100
	}

	poolCtx, cancel := context.WithCancel(ctx)

	pool := &WorkerPool{
		jobs:    make(chan *nats.Msg, queueSize),
		ctx:     poolCtx,
		cancel:  cancel,
		handler: handler,
	}

	for i := 0; i < maxWorkers; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	return pool
}

func (w *WorkerPool) worker() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return
		case msg, ok := <-w.jobs:
			if !ok {
				return
			}
			w.processMessage(msg)
		}
	}
}

func (w *WorkerPool) processMessage(msg *nats.Msg) {
	if err := w.handler(w.ctx, msg.Data); err != nil {
		slog.Error("failed to handle message", "err", err)
		if err := msg.Nak(); err != nil {
			slog.Error("failed to nak message", "err", err)
		}
		return
	}

	if err := msg.Ack(); err != nil {
		slog.Error("failed to ack message", "err", err)
	}
}

// Submit sends a message to the worker pool. Blocks if queue is full (backpressure).
// Returns false if context is cancelled.
func (w *WorkerPool) Submit(ctx context.Context, msg *nats.Msg) bool {
	select {
	case w.jobs <- msg:
		return true
	case <-ctx.Done():
		return false
	case <-w.ctx.Done():
		return false
	}
}

func (w *WorkerPool) Stop() {
	w.cancel()
	close(w.jobs)
}

func (w *WorkerPool) Wait() {
	w.wg.Wait()
}
