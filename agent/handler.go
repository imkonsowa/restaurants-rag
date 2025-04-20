package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/imkonsowa/restaurants-rag/models"
	_ "github.com/lib/pq"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
)

type ParsedInput struct {
	Query      string   `json:"query"`      // cleaned query for semantic search
	Distance   *float64 `json:"distance"`   // in meters, nil if not specified
	Rating     *float64 `json:"rating"`     // 1-5 scale, nil if not specified
	Confidence float64  `json:"confidence"` // 0-1 scale for parsing confidence
}

type Handler struct {
	contextLLM   *chains.LLMChain
	embeddingLLM *ollama.LLM
	parserLLM    *ollama.LLM
	pg           *Pg
}

func NewHandler(db *Pg, contextLLM *chains.LLMChain, embeddingLLM, parserLLM *ollama.LLM) (*Handler, error) {
	return &Handler{
		contextLLM:   contextLLM,
		embeddingLLM: embeddingLLM,
		parserLLM:    parserLLM,
		pg:           db,
	}, nil
}

func (h *Handler) CreateRestaurants(
	ctx context.Context,
	restaurants []models.RestaurantWithMenuItems,
) error {
	if len(restaurants) == 0 {
		return fmt.Errorf("no restaurants provided")
	}

	return h.pg.Create(ctx, restaurants)
}

func (h *Handler) ListRestaurants(ctx context.Context) ([]models.Restaurant, error) {
	restaurants, err := h.pg.ListRestaurants(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list restaurants: %w", err)
	}

	return restaurants, nil
}

func (h *Handler) SearchByUserQuery(
	ctx context.Context,
	userInput string,
	location *GeoPoint,
) chan *ProcessingResult {
	resultChan := make(chan *ProcessingResult)

	go func() {
		defer func() {
			close(resultChan)
		}()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		parsed, err := h.Parse(ctx, userInput)
		if err != nil {
			resultChan <- &ProcessingResult{
				Err: fmt.Errorf("failed to parse user input: %w", err),
			}

			return
		}

		resultChan <- &ProcessingResult{
			Msg: WebSocketsMessage{
				Type: "debug",
				Data: parsed,
			},
		}

		filter := SearchFilter{
			MaxDistance: DefaultMaxDistance,
			MinRating:   DefaultMinRating,
			CurrentTime: time.Now(),
			Location:    location,
		}

		if parsed.Distance != nil {
			filter.MaxDistance = *parsed.Distance
		}
		if parsed.Rating != nil {
			filter.MinRating = *parsed.Rating
		}

		queryVector, err := h.embeddingLLM.CreateEmbedding(ctx, []string{userInput})
		if err != nil {
			resultChan <- &ProcessingResult{
				Err: fmt.Errorf("failed to generate query embedding: %w", err),
			}

			return
		}
		if len(queryVector) == 0 {
			resultChan <- &ProcessingResult{
				Msg: WebSocketsMessage{
					Type: "chat",
					Data: "I couldn't understand your query.",
				},
			}

			return
		}

		results, err := h.pg.Search(ctx, queryVector[0], filter)
		if err != nil {
			slog.Error("failed to search restaurants in db", "error", err)

			resultChan <- &ProcessingResult{
				Err: fmt.Errorf("search failed: %w", err),
			}

			return
		}

		if len(results) == 0 {
			resultChan <- &ProcessingResult{
				Msg: WebSocketsMessage{
					Type: "chat",
					Data: "I couldn't find any restaurants matching your criteria.",
				},
			}

			return
		}

		res, err := json.Marshal(map[string]interface{}{
			"results": results,
		})
		if err != nil {
			resultChan <- &ProcessingResult{
				Err: fmt.Errorf("failed to marshal results: %w", err),
			}

			return
		}

		resultChan <- &ProcessingResult{
			Msg: WebSocketsMessage{
				Type: "restaurants",
				Data: string(res),
			},
		}

		_, err = h.GenerateSummary(ctx, userInput, results, func(message []byte) error {
			resultChan <- &ProcessingResult{
				Err: nil,
				Msg: WebSocketsMessage{
					Type: "chat",
					Data: string(message),
				},
			}

			return nil
		})
		if err != nil {
			resultChan <- &ProcessingResult{
				Err: fmt.Errorf("response generation failed: %w", err),
			}
			return
		}

		resultChan <- &ProcessingResult{
			Err: io.EOF,
		}

		return
	}()

	return resultChan
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

func (h *Handler) GenerateSummary(
	ctx context.Context,
	userInput string,
	restaurants []models.RestaurantWithMenuItems,
	streamHandler func(message []byte) error,
) (string, error) {
	summary := createRestaurantSummary(userInput, restaurants)

	finalResponse, err := chains.Run(
		ctx,
		h.contextLLM,
		summary,
		chains.WithTemperature(0),
		chains.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			return streamHandler(chunk)
		}),
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate response: %w", err)
	}

	return finalResponse, nil
}
func createRestaurantSummary(userInput string, restaurants []models.RestaurantWithMenuItems) string {
	var summary strings.Builder

	summary.WriteString("The user asked me to find restaurants for them. with this prompt: " + userInput + "\n")
	summary.WriteString("Here are the restaurants I found:\n")

	for _, restaurant := range restaurants {
		summary.WriteString(restaurant.Restaurant.Stringify())
		summary.WriteString("\n")
		for _, item := range restaurant.MenuItems {
			summary.WriteString("\t\t" + item.Stringify())
		}

		summary.WriteString("--------------------\n")
	}

	return summary.String()
}

type WebSocketsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func (h *Handler) Parse(ctx context.Context, input string) (*ParsedInput, error) {
	prompt := fmt.Sprintf("Parse this search query and return only valid JSON: %q", input)

	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				//				llms.TextPart(`You are an assistant that responds with a must be valid JSON object contains distance key and rating key for restaurant.
				//For "nearby" → set distance to 1000 (meters)
				//For "highly rated" → set rating to 4.0
				//ONLY output valid JSON, no other text
				//If parameter not found, omit it from the json body
				//`)
				llms.TextPart(ParserSysPrompt),
			},
		},
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextPart(prompt),
			},
		},
	}
	content, err := h.parserLLM.GenerateContent(
		ctx,
		messages,
		llms.WithJSONMode(),
		llms.WithTemperature(0),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	var parsed ParsedInput
	if content.Choices == nil {
		return &parsed, nil
	}

	payload := content.Choices[0].Content
	json.Unmarshal([]byte(payload), &parsed)

	// Validate parsed output
	if err := h.validateParsedInput(&parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}

func (h *Handler) validateParsedInput(input *ParsedInput) error {
	if input.Distance != nil && *input.Distance <= 0 {
		return fmt.Errorf("distance must be positive")
	}

	if input.Rating != nil {
		if *input.Rating < 1 || *input.Rating > 5 {
			return fmt.Errorf("rating must be between 1 and 5")
		}
	}

	return nil
}
