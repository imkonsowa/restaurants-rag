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

func (c *Client) Subscribe(ctx context.Context, subject string, handler func(m *nats.Msg)) error {
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
			msgs, err := subscription.Fetch(4, nats.MaxWait(200*time.Millisecond))
			// TODO: add retries
			if err != nil && !errors.Is(err, nats.ErrTimeout) {
				return err
			}
			if len(msgs) == 0 {
				continue
			}

			for _, msg := range msgs {
				handler(msg)
			}
		}
	}
}
