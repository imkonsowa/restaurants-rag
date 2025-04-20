package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	_ "github.com/lib/pq"
	"github.com/tmc/langchaingo/llms/ollama"
)

type Handler struct {
	llm *ollama.LLM
	pg  *Pg
}

func NewHandler(llm *ollama.LLM, pg *Pg) (*Handler, error) {
	return &Handler{
		llm: llm,
		pg:  pg,
	}, nil
}

func (h *Handler) GenerateTextVector(ctx context.Context, text string) ([]float32, error) {
	embeds, err := h.llm.CreateEmbedding(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding: %w", err)
	}
	if len(embeds) == 0 {
		return nil, fmt.Errorf("empty embeddings")
	}

	return embeds[0], nil
}

// HandleRestaurantCDCMessage Updates restaurant vector in the database on receiving a cdc message from nats.
func (h *Handler) HandleRestaurantCDCMessage(ctx context.Context, msg []byte) error {
	var data map[string]interface{}

	err := json.Unmarshal(msg, &data)
	if err != nil {
		return err
	}

	restaurantId := uint64(data["id"].(float64))

	restaurant, err := h.pg.GetRestaurant(ctx, restaurantId)
	if err != nil {
		return err
	}

	vector, err := h.GenerateTextVector(ctx, restaurant.Stringify())
	if err != nil {
		slog.Warn("Failed to generate restaurant vector", "err", err)
	}

	if err := h.pg.UpdateRestaurantVector(ctx, restaurantId, vectorToStr(vector)); err != nil {
		slog.Warn("Failed to update restaurant vector", "err", err)
	}

	return nil
}

func (h *Handler) HandleMenuItemCDCMessage(ctx context.Context, msg []byte) error {
	var data map[string]interface{}

	err := json.Unmarshal(msg, &data)
	if err != nil {
		return err
	}

	menuItemId := uint64(data["id"].(float64))

	menuItem, err := h.pg.GetMenuItem(ctx, menuItemId)
	if err != nil {
		return err
	}

	vector, err := h.GenerateTextVector(ctx, menuItem.Stringify())
	if err != nil {
		slog.Warn("Failed to generate menu item vector", "err", err)
	}

	if err := h.pg.UpdateMenuItemVector(ctx, menuItemId, vectorToStr(vector)); err != nil {
		slog.Warn("Failed to update menu item vector", "err", err)
	}

	return nil
}

func (h *Handler) HandleCategoryCDCMessage(ctx context.Context, msg []byte) error {
	var data map[string]interface{}

	err := json.Unmarshal(msg, &data)
	if err != nil {
		return err
	}

	categoryId := uint64(data["id"].(float64))

	category, err := h.pg.GetCategory(ctx, categoryId)
	if err != nil {
		return err
	}

	vector, err := h.GenerateTextVector(ctx, category.Stringify())
	if err != nil {
		slog.Warn("Failed to generate category vector", "err", err)
	}

	if err := h.pg.UpdateCategoryVector(ctx, categoryId, vectorToStr(vector)); err != nil {
		slog.Warn("Failed to update category vector", "err", err)
	}

	return nil
}

func normalizeVector(vec []float32) []float32 {
	var sum float32
	for _, v := range vec {
		sum += v * v
	}
	norm := float32(math.Sqrt(float64(sum)))
	for i := range vec {
		vec[i] /= norm
	}

	return vec
}

func vectorToStr(vector []float32) string {
	normalizeVector(vector)

	vectorStr := "["
	for i, v := range vector {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%f", v)
	}
	vectorStr += "]"

	return vectorStr
}
