package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/imkonsowa/restaurants-rag/config"
	"github.com/nats-io/nats.go"
)

type Client struct {
	conn *nats.Conn
	js   nats.JetStreamContext
}

func NewNats(cfg *config.Config) (*Client, error) {
	nc, err := nats.Connect(cfg.Nats.ConnStr())
	if err != nil {
		return nil, err
	}

	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}

	return &Client{
		conn: nc,
		js:   js,
	}, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Subscribe(ctx context.Context, subject string, pool *WorkerPool) error {
	subscription, err := c.js.PullSubscribe(subject, strings.ReplaceAll(subject+".consumer", ".", "-"), nats.ManualAck())
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			if err := subscription.Unsubscribe(); err != nil {
				slog.Warn("failed to unsubscribe from subject", "subject", subject, "error", err)
			}
			return nil
		default:
			msgs, err := subscription.Fetch(10, nats.MaxWait(200*time.Millisecond))
			if err != nil && !errors.Is(err, nats.ErrTimeout) {
				return err
			}
			for _, msg := range msgs {
				if !pool.Submit(ctx, msg) {
					return nil
				}
			}
		}
	}
}
