package main

import (
	"context"

	"github.com/imkonsowa/restaurants-rag/models"
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Pg struct {
	db *gorm.DB
}

func NewPg(connString string) (*Pg, error) {
	db, err := gorm.Open(postgres.Open(connString), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	return &Pg{
		db: db,
	}, nil
}

func (p *Pg) GetRestaurant(ctx context.Context, restaurantId uint64) (*models.Restaurant, error) {
	var restaurant models.Restaurant
	if err := p.db.WithContext(ctx).Find(&restaurant, "id = ?", restaurantId).Omit("location").Error; err != nil {
		return nil, err
	}

	return &restaurant, nil
}

func (p *Pg) GetMenuItem(ctx context.Context, menuItemId uint64) (*models.MenuItem, error) {
	var menuItem models.MenuItem
	if err := p.db.WithContext(ctx).Find(&menuItem, "id = ?", menuItemId).Error; err != nil {
		return nil, err
	}

	return &menuItem, nil
}

func (p *Pg) GetCategory(ctx context.Context, categoryId uint64) (*models.Category, error) {
	var category models.Category
	if err := p.db.WithContext(ctx).Find(&category, "id = ?", categoryId).Error; err != nil {
		return nil, err
	}

	return &category, nil
}

func (p *Pg) UpdateRestaurantVector(ctx context.Context, restaurantId uint64, vector pgvector.Vector) error {
	return p.db.WithContext(ctx).Model(&models.Restaurant{}).Where("id = ?", restaurantId).Update("embedding", vector).Error
}

func (p *Pg) UpdateMenuItemVector(ctx context.Context, menuItemId uint64, vector pgvector.Vector) error {
	return p.db.WithContext(ctx).Model(&models.MenuItem{}).Where("id = ?", menuItemId).Update("embedding", vector).Error
}

func (p *Pg) UpdateCategoryVector(ctx context.Context, categoryId uint64, vector pgvector.Vector) error {
	return p.db.WithContext(ctx).Model(&models.Category{}).Where("id = ?", categoryId).Update("embedding", vector).Error
}
