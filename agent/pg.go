package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/imkonsowa/restaurants-rag/models"
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

type SearchFilter struct {
	Cuisine     string    `json:"cuisine,omitempty"`
	PriceRange  string    `json:"price_range,omitempty"`
	MinRating   float64   `json:"min_rating,omitempty"`
	CurrentTime time.Time `json:"current_time,omitempty"`
	MaxDistance float64   `json:"max_distance"`
	Location    *GeoPoint `json:"location"`
}

func (s *Pg) Search(
	ctx context.Context,
	queryVector []float32,
	filter SearchFilter,
) ([]models.RestaurantWithMenuItems, error) {
	vectorStr := vectorToStr(queryVector)

	// Step 1: Find matching restaurants
	var matchingRestaurants []models.Restaurant
	restaurantQuery := s.db.WithContext(ctx).
		Model(&models.Restaurant{}).
		Select("*, 1 - (embedding <=> ?) as similarity", vectorStr).
		Where("1 - (embedding <=> ?) >= 0.9", vectorStr).
		Order("similarity DESC").
		Limit(10)

	menuQuery := s.db.WithContext(ctx).
		Table("menu_items").
		Select("menu_items.*, restaurant_id, 1 - (menu_items.embedding <=> ?) as mi_similarity", vectorStr).
		Where("1 - (menu_items.embedding <=> ?) >= 0.4", vectorStr).
		Order("mi_similarity DESC").
		Joins("JOIN restaurants ON menu_items.restaurant_id = restaurants.id").
		Limit(10)

	if filter.MaxDistance > 0 && filter.Location != nil {
		restaurantQuery = restaurantQuery.Where("ST_Distance(location::geography, ST_SetSRID(ST_MakePoint(?, ?), 4326)) <= ?", filter.Location.Lat, filter.Location.Long, filter.MaxDistance)

		menuQuery = menuQuery.Where("ST_Distance(restaurants.location::geography, ST_SetSRID(ST_MakePoint(?, ?), 4326)) <= ?", filter.Location.Lat, filter.Location.Long, filter.MaxDistance)
	}
	if filter.MinRating > 0 {
		restaurantQuery = restaurantQuery.Where("rating >= ?", filter.MinRating)

		menuQuery = menuQuery.Where("restaurants.rating >= ?", filter.MinRating)
	}

	if err := restaurantQuery.Find(&matchingRestaurants).Error; err != nil {
		return nil, fmt.Errorf("failed to query matching restaurants: %w", err)
	}

	// Step 2: Find matching menu items
	var matchingMenuItems []struct {
		models.MenuItem
		RestaurantID uint64
		Similarity   float64
	}

	if err := menuQuery.Scan(&matchingMenuItems).Error; err != nil {
		return nil, fmt.Errorf("failed to query matching menu items: %w", err)
	}

	// Step 3: Group menu items by restaurant
	menuItemsByRestaurant := make(map[uint64][]models.MenuItem)
	for _, item := range matchingMenuItems {
		menuItemsByRestaurant[item.RestaurantID] = append(
			menuItemsByRestaurant[item.RestaurantID],
			item.MenuItem,
		)
	}

	// Step 4: Create a set of unique restaurant IDs that matched either by restaurant or menu item
	matchedRestaurantIDs := make(map[uint64]bool)

	// Add restaurants that matched directly
	for _, r := range matchingRestaurants {
		matchedRestaurantIDs[r.ID] = true
	}

	// Add restaurants that matched via menu items
	for _, item := range matchingMenuItems {
		matchedRestaurantIDs[item.RestaurantID] = true
	}

	// Step 5: For any restaurants that matched via menu items but weren't in our direct restaurant matches,
	// we need to fetch them separately
	var additionalRestaurantIDs []uint64
	for id := range matchedRestaurantIDs {
		found := false
		for _, r := range matchingRestaurants {
			if r.ID == id {
				found = true
				break
			}
		}
		if !found {
			additionalRestaurantIDs = append(additionalRestaurantIDs, id)
		}
	}

	if len(additionalRestaurantIDs) > 0 {
		var additionalRestaurants []models.Restaurant
		if err := s.db.WithContext(ctx).
			Where("id IN ?", additionalRestaurantIDs).
			Find(&additionalRestaurants).Error; err != nil {
			return nil, fmt.Errorf("failed to fetch additional restaurants: %w", err)
		}
		matchingRestaurants = append(matchingRestaurants, additionalRestaurants...)
	}

	//var restaurantIds []uint64
	//for _, restaurant := range matchingRestaurants {
	//	restaurantIds = append(restaurantIds, restaurant.ID)
	//}
	//
	//var allMenuItems []models.MenuItem
	//if err := s.db.WithContext(ctx).
	//	Where("restaurant_id IN ?", restaurantIds).
	//	Find(&allMenuItems).Error; err != nil {
	//	return nil, fmt.Errorf("failed to fetch menu items: %w", err)
	//}
	//
	//// Group menu items by restaurant
	//for _, item := range allMenuItems {
	//	menuItemsByRestaurant[item.RestaurantID] = append(
	//		menuItemsByRestaurant[item.RestaurantID],
	//		item,
	//	)
	//}

	// Step 6: Assemble the final results
	var results []models.RestaurantWithMenuItems
	for _, restaurant := range matchingRestaurants {
		menuItems := menuItemsByRestaurant[restaurant.ID]
		results = append(results, models.RestaurantWithMenuItems{
			Restaurant: restaurant,
			MenuItems:  menuItems,
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
