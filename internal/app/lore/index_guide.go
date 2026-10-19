package loreapp

import (
	"context"
	"denova/internal/book/lore"
)

func (service *Service) IndexGuide(ctx context.Context, projectID string) (lore.IndexGuideSnapshot, error) {
	var result lore.IndexGuideSnapshot
	_, err := service.withStore(ctx, projectID, func(store *lore.Store) error {
		var err error
		result, err = store.IndexGuide()
		return err
	})
	return result, err
}

func (service *Service) UpdateIndexGuide(ctx context.Context, projectID string, input lore.IndexGuideUpdate) (lore.IndexGuideSnapshot, error) {
	var result lore.IndexGuideSnapshot
	_, err := service.withStore(ctx, projectID, func(store *lore.Store) error {
		var err error
		result, err = store.UpdateIndexGuide(input)
		return err
	})
	return result, err
}

func (service *Service) PreviewIndexGuide(ctx context.Context, projectID string, guide lore.IndexGuide) (lore.IndexPreview, error) {
	var result lore.IndexPreview
	_, err := service.withStore(ctx, projectID, func(store *lore.Store) error {
		var err error
		result, err = store.PreviewIndexGuide(guide)
		return err
	})
	return result, err
}
