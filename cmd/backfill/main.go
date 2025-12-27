package main

import (
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"time"

	"github.com/imkonsowa/restaurants-rag/config"
	"github.com/nats-io/nats.go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type CDCMessage struct {
	Table string `json:"table"`
	Kind  string `json:"kind"`
	ID    uint64 `json:"id"`
}

func main() {
	cfg := config.LoadConfig()

	db, err := gorm.Open(postgres.Open(cfg.Postgres.ConnStr()), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect to postgres:", err)
	}

	nc, err := nats.Connect(cfg.Nats.ConnStr())
	if err != nil {
		log.Fatal("failed to connect to nats:", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatal("failed to get jetstream context:", err)
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:      cfg.Nats.Stream,
		Subjects:  []string{cfg.Nats.RestaurantsSubject, cfg.Nats.MenuItemsSubject, cfg.Nats.CategoriesSubject},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    time.Hour * 24 * 7,
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		log.Fatal("failed to create stream:", err)
	}

	var menuItemIDs []uint64
	if err := db.Table("menu_items").Where("embedding IS NULL").Pluck("id", &menuItemIDs).Error; err != nil {
		log.Fatal("failed to query unembedded menu items:", err)
	}
	slog.Info("found unembedded menu items", "count", len(menuItemIDs))

	for _, id := range menuItemIDs {
		msg := CDCMessage{
			Table: "menu_items",
			Kind:  "insert",
			ID:    id,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			slog.Error("failed to marshal message", "err", err)
			continue
		}
		if _, err := js.Publish(cfg.Nats.MenuItemsSubject, data); err != nil {
			slog.Error("failed to publish menu item", "id", id, "err", err)
			continue
		}
		slog.Info("published menu item for embedding", "id", id)
	}

	var restaurantIDs []uint64
	if err := db.Table("restaurants").Where("embedding IS NULL").Pluck("id", &restaurantIDs).Error; err != nil {
		log.Fatal("failed to query unembedded restaurants:", err)
	}
	slog.Info("found unembedded restaurants", "count", len(restaurantIDs))

	for _, id := range restaurantIDs {
		msg := CDCMessage{
			Table: "restaurants",
			Kind:  "insert",
			ID:    id,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			slog.Error("failed to marshal message", "err", err)
			continue
		}
		if _, err := js.Publish(cfg.Nats.RestaurantsSubject, data); err != nil {
			slog.Error("failed to publish restaurant", "id", id, "err", err)
			continue
		}
		slog.Info("published restaurant for embedding", "id", id)
	}

	slog.Info("backfill complete", "menu_items", len(menuItemIDs), "restaurants", len(restaurantIDs))
}
