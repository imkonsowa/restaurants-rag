package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/imkonsowa/restaurants-rag/models"
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Pg struct {
	db *gorm.DB
}

func NewRestaurantPg(connStr string) (*Pg, error) {
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Silent,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  true,
		},
	)

	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		return nil, err
	}

	return &Pg{db: db}, nil
}

type SearchFilter struct {
	PriceRange  string    `json:"price_range,omitempty"`
	MinRating   float64   `json:"min_rating,omitempty"`
	MaxDistance float64   `json:"max_distance"`
	Location    *GeoPoint `json:"location"`
}

func (s *Pg) Search(
	ctx context.Context,
	queryVector []float32,
	filter SearchFilter,
) ([]models.RestaurantWithMenuItems, error) {
	vec := pgvector.NewVector(queryVector)

	query := s.db.WithContext(ctx).
		Table("menu_items").
		Select("menu_items.*, restaurant_id, 1 - (menu_items.embedding <=> ?) as similarity", vec).
		Where("1 - (menu_items.embedding <=> ?) >= 0.6", vec).
		Order("similarity DESC").
		Joins("JOIN restaurants ON menu_items.restaurant_id = restaurants.id")

	if filter.MaxDistance > 0 && filter.Location != nil {
		query = query.Where(
			"ST_Distance(restaurants.location::geography, ST_SetSRID(ST_MakePoint(?, ?), 4326)) <= ?",
			filter.Location.Lat, filter.Location.Long, filter.MaxDistance,
		)
	}
	if filter.MinRating > 0 {
		query = query.Where("restaurants.rating >= ?", filter.MinRating)
	}

	var matchingItems []struct {
		models.MenuItem
		RestaurantID uint64
		Similarity   float64
	}
	if err := query.Scan(&matchingItems).Error; err != nil {
		return nil, fmt.Errorf("query menu items: %w", err)
	}

	type restaurantMatch struct {
		items          []models.MenuItem
		bestSimilarity float64
	}
	matches := make(map[uint64]*restaurantMatch)
	var orderedIDs []uint64

	for _, item := range matchingItems {
		m, exists := matches[item.RestaurantID]
		if !exists {
			m = &restaurantMatch{}
			matches[item.RestaurantID] = m
			orderedIDs = append(orderedIDs, item.RestaurantID)
		}
		m.items = append(m.items, item.MenuItem)
		if item.Similarity > m.bestSimilarity {
			m.bestSimilarity = item.Similarity
		}
	}

	if len(orderedIDs) == 0 {
		return nil, nil
	}
	if len(orderedIDs) > 10 {
		orderedIDs = orderedIDs[:10]
	}

	var restaurants []models.Restaurant
	if err := s.db.WithContext(ctx).Where("id IN ?", orderedIDs).Find(&restaurants).Error; err != nil {
		return nil, fmt.Errorf("fetch restaurants: %w", err)
	}

	restaurantMap := make(map[uint64]models.Restaurant)
	for _, r := range restaurants {
		restaurantMap[r.ID] = r
	}

	results := make([]models.RestaurantWithMenuItems, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		results = append(results, models.RestaurantWithMenuItems{
			Restaurant: restaurantMap[id],
			MenuItems:  matches[id].items,
		})
	}

	return results, nil
}

func (s *Pg) Create(
	ctx context.Context,
	items []models.RestaurantWithMenuItems,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		for _, item := range items {
			var menuItems []models.MenuItem

			if err := tx.Debug().Create(&item.Restaurant).Error; err != nil {
				return fmt.Errorf("failed to create restaurant: %w", err)
			}

			for _, menuItem := range item.MenuItems {
				menuItem.RestaurantID = item.Restaurant.ID
				menuItems = append(menuItems, menuItem)
			}

			if err := tx.Debug().Create(&menuItems).Error; err != nil {
				return fmt.Errorf("failed to create menu items: %w", err)
			}
		}

		return nil
	})
}

func (s *Pg) ListRestaurants(ctx context.Context) ([]models.Restaurant, error) {
	var restaurants []models.Restaurant
	if err := s.db.WithContext(ctx).Find(&restaurants).Error; err != nil {
		return nil, fmt.Errorf("failed to list restaurants: %w", err)
	}

	return restaurants, nil
}
