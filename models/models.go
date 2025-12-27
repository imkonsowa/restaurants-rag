package models

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Location struct {
	Lon, Lat float64
}

func NewGeoPoint(lng, lat float64) Location {
	return Location{
		Lon: lng,
		Lat: lat,
	}
}

func (g *Location) Scan(value interface{}) error {
	var data []byte
	switch v := value.(type) {
	case string:
		var err error
		data, err = hex.DecodeString(v)
		if err != nil {
			return err
		}
	case []byte:
		data = v
	default:
		return fmt.Errorf("expected string or []byte, got %T", value)
	}

	t, err := ewkb.Unmarshal(data)
	if err != nil {
		return err
	}

	if point, ok := t.(*geom.Point); ok {
		g.Lon = point.X()
		g.Lat = point.Y()

		return nil
	}

	return fmt.Errorf("expected Point, got %T", t)
}
func (loc Location) GormDataType() string {
	return "geometry"
}

func (loc Location) GormValue(ctx context.Context, db *gorm.DB) clause.Expr {
	return clause.Expr{
		SQL:  "ST_PointFromText(?)",
		Vars: []interface{}{fmt.Sprintf("POINT(%f %f)", loc.Lon, loc.Lat)},
	}
}

type Restaurant struct {
	ID        uint64          `gorm:"primaryKey" json:"id"`
	Name      string          `json:"name"`
	Area      string          `json:"area"`
	Rating    float64         `json:"rating"`
	Badges    pq.StringArray  `gorm:"type:text[]" json:"badges"`
	Location  Location        `json:"location"`
	Embedding pgvector.Vector `gorm:"type:vector(768)" json:"-"`
}

func (r *Restaurant) TableName() string {
	return "restaurants"
}

func (r *Restaurant) Stringify() string {
	return fmt.Sprintf("Restaurant: %s, Area: %s, Rating: %.1f, Badges: %s", r.Name, r.Area, r.Rating, strings.Join(r.Badges, ", "))
}

type Category struct {
	ID           uint            `gorm:"primaryKey" json:"id"`
	RestaurantID uint            `json:"restaurant_id"`
	Name         string          `json:"name"`
	Embedding    pgvector.Vector `gorm:"type:vector(768)" json:"-"`
}

func (c *Category) TableName() string {
	return "categories"
}

func (c *Category) Stringify() string {
	return fmt.Sprintf("Category: %s", c.Name)
}

type MenuItem struct {
	ID           uint64          `gorm:"primaryKey" json:"id"`
	RestaurantID uint64          `json:"restaurant_id"`
	Category     string          `json:"category"`
	Name         string          `json:"name"`
	Price        float64         `json:"price"`
	Description  string          `json:"description"`
	Embedding    pgvector.Vector `gorm:"type:vector(768)" json:"-"`
}

func (m *MenuItem) TableName() string {
	return "menu_items"
}

func (m *MenuItem) Stringify() string {
	return fmt.Sprintf("MenuItem: %s, Category: %s, Price: %.2f, Description: %s", m.Name, m.Category, m.Price, m.Description)
}

type RestaurantWithMenuItems struct {
	Restaurant Restaurant `json:"restaurant"`
	MenuItems  []MenuItem `json:"menu_items,omitempty"`
}
