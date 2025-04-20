package main

import (
	"errors"
	"time"

	"github.com/imkonsowa/restaurants-rag/config"
	"github.com/nats-io/nats.go"
)

type NatsClient struct {
	conn *nats.Conn
	js   nats.JetStreamContext
}

func NewNatsClient(cfg *config.Nats) (*NatsClient, error) {
	nc, err := nats.Connect(cfg.ConnStr())
	if err != nil {
		return nil, err
	}

	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:      cfg.Stream,
		Subjects:  []string{cfg.RestaurantsSubject, cfg.MenuItemsSubject, cfg.CategoriesSubject},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    time.Hour * 24 * 7,
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return nil, err
	}

	return &NatsClient{conn: nc, js: js}, err
}

func (c *NatsClient) Close() {
	c.conn.Close()
}

func (c *NatsClient) Publish(subject string, data []byte) error {
	_, err := c.js.PublishAsync(subject, data)

	return err
}
