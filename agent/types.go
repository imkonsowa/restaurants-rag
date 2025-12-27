package main

import (
	"fmt"

	"github.com/imkonsowa/restaurants-rag/models"
)

const (
	DefaultNearbyDistance = 1000  // 1km for "nearby"
	DefaultHighRating     = 4     // for "highly rated"
	DefaultMaxDistance    = 20000 // 5km default max
	DefaultMinRating      = 3     // default minimum rating
)

type GeoPoint struct {
	Lat  float64 `json:"lat"`
	Long float64 `json:"long"`
}

type ProcessingResult struct {
	Err error
	Msg WebSocketsMessage
}

type CreateRestaurantsRequest struct {
	Restaurants []struct {
		Name      string   `json:"name"`
		Area      string   `json:"area"`
		Location  GeoPoint `json:"location"`
		Rating    float64  `json:"rating"`
		Badges    []string `json:"badges"`
		MenuItems []struct {
			Name        string  `json:"name"`
			Category    string  `json:"category"`
			Description string  `json:"description"`
			Price       float64 `json:"price"`
		} `json:"menu_items"`
	}
}

func (c *CreateRestaurantsRequest) Validate() error {
	if len(c.Restaurants) == 0 {
		return fmt.Errorf("no restaurants provided")
	}

	for _, r := range c.Restaurants {
		if r.Name == "" || r.Area == "" || r.Rating == 0 {
			return fmt.Errorf("restaurant name, area, and rating are required")
		}
		for _, m := range r.MenuItems {
			if m.Name == "" || m.Description == "" || m.Price == 0 {
				return fmt.Errorf("menu item name, description, and price are required")
			}
		}
	}

	return nil
}

func (c *CreateRestaurantsRequest) ToModels() []models.RestaurantWithMenuItems {
	restaurants := make([]models.RestaurantWithMenuItems, len(c.Restaurants))
	for i, r := range c.Restaurants {
		restaurants[i] = models.RestaurantWithMenuItems{
			Restaurant: models.Restaurant{
				Name:     r.Name,
				Area:     r.Area,
				Badges:   r.Badges,
				Location: models.NewGeoPoint(r.Location.Lat, r.Location.Long),
				Rating:   r.Rating,
			},
			MenuItems: make([]models.MenuItem, len(r.MenuItems)),
		}

		for j, m := range r.MenuItems {
			restaurants[i].MenuItems[j] = models.MenuItem{
				Name:        m.Name,
				Description: m.Description,
				Price:       m.Price,
				Category:    m.Category,
			}
		}
	}

	return restaurants
}
