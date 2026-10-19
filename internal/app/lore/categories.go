package loreapp

import (
	"context"
	"denova/internal/book/lore"
)

func (service *Service) Categories(ctx context.Context, projectID string) ([]lore.Category, error) {
	var categories []lore.Category
	_, err := service.withStore(ctx, projectID, func(store *lore.Store) error {
		var err error
		categories, err = store.Categories()
		return err
	})
	return categories, err
}

func (service *Service) MutateCategory(ctx context.Context, projectID string, input lore.CategoryMutation) ([]lore.Category, error) {
	var categories []lore.Category
	_, err := service.withStore(ctx, projectID, func(store *lore.Store) error {
		var err error
		categories, err = store.MutateCategory(input)
		return err
	})
	return categories, err
}
