package main

import (
	"database/sql"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/imkonsowa/restaurants-rag/config"
	_ "github.com/mattn/go-sqlite3"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/memory"
	"github.com/tmc/langchaingo/memory/sqlite3"
)

type Agent struct {
	config   *config.Config
	handler  *Handler
	upgrader websocket.Upgrader
}

func main() {
	cfg := config.LoadConfig()

	sqliteDb, err := sql.Open("sqlite3", "chat_history.db")
	if err != nil {
		log.Fatal()
	}

	chatHistory := sqlite3.NewSqliteChatMessageHistory(
		sqlite3.WithSession("restaurants-agent"),
		sqlite3.WithDB(sqliteDb),
	)
	conversationBuffer := memory.NewConversationBuffer(memory.WithChatHistory(chatHistory))

	db, err := NewRestaurantPg(cfg.Postgres.ConnStr())
	if err != nil {
		log.Fatal(err)
	}
	embeddingLLM, err := ollama.New(
		ollama.WithServerURL(cfg.Ollama.Address()),
		ollama.WithModel(cfg.Ollama.EmbeddingModel),
	)
	if err != nil {
		log.Fatal(err)
	}

	parserLLM, err := ollama.New(
		ollama.WithServerURL(cfg.Ollama.Address()),
		ollama.WithModel(cfg.Ollama.ParserModel),
		// ollama.WithSystemPrompt(ParserSysPrompt),
	)
	if err != nil {
		log.Fatal(err)
	}

	contextLLM, err := ollama.New(
		ollama.WithServerURL(cfg.Ollama.Address()),
		ollama.WithModel(cfg.Ollama.ContextModel),
		ollama.WithSystemPrompt(ContextSysPrompt),
	)
	if err != nil {
		log.Fatal(err)
	}

	llmChain := chains.NewConversation(contextLLM, conversationBuffer)

	handler, err := NewHandler(db, &llmChain, embeddingLLM, parserLLM)
	if err != nil {
		log.Fatal(err)
	}

	agent := &Agent{
		handler:  handler,
		config:   cfg,
		upgrader: websocket.Upgrader{},
	}

	if err := agent.Run(); err != nil {
		log.Fatalf("failed to run the agent: %v", err)
	}
}

func (a *Agent) Run() error {
	r := gin.Default()

	r.StaticFile("/", "web/index.html")

	r.GET("/search", func(ctx *gin.Context) {
		input, _ := ctx.GetQuery("input")
		longitude, _ := ctx.GetQuery("longitude")
		latitude, _ := ctx.GetQuery("latitude")

		w, r := ctx.Writer, ctx.Request
		c, err := a.upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrade:", err)
			return
		}
		defer c.Close()

		var point *GeoPoint

		if longitude != "" && latitude != "" {
			point = &GeoPoint{}

			point.Lat, err = strconv.ParseFloat(latitude, 64)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid latitude"})
				return
			}

			point.Long, err = strconv.ParseFloat(longitude, 64)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid longitude"})
				return
			}
		}

		resultChan := a.handler.SearchByUserQuery(ctx, input, point)
		for {
			select {
			case <-ctx.Request.Context().Done():
				return
			case result := <-resultChan:
				if result == nil {
					return
				}
				if result.Err != nil {
					if result.Err == io.EOF {
						return
					}
					ctx.JSON(http.StatusInternalServerError, gin.H{"error": result.Err.Error()})
					return
				}

				if err := c.WriteJSON(result.Msg); err != nil {
					slog.Error("failed to write to ws connection", "error", err)
					return
				}
			}
		}
	})

	r.POST("/restaurants", func(context *gin.Context) {
		var restaurants CreateRestaurantsRequest

		if err := context.ShouldBindJSON(&restaurants); err != nil {
			context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := restaurants.Validate(); err != nil {
			context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := a.handler.CreateRestaurants(context, restaurants.ToModels()); err != nil {
			context.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		context.JSON(http.StatusCreated, gin.H{"message": "restaurants created successfully"})
	})

	r.GET("/restaurants", func(context *gin.Context) {
		restaurants, err := a.handler.ListRestaurants(context)
		if err != nil {
			context.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		context.JSON(http.StatusOK, restaurants)
	})

	return r.Run(a.config.Server.Address())
}
