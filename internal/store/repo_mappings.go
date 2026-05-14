package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// RepoMappings persists the mapping of GitHub repository → Slack channel +
// mentions.
type RepoMappings struct {
	db *gorm.DB
}

// NewRepoMappings constructs a RepoMappings repository bound to db.
func NewRepoMappings(db *gorm.DB) *RepoMappings {
	return &RepoMappings{db: db}
}

// Upsert creates or replaces the mapping keyed by repository. The returned
// RepoMapping carries the persisted ID.
func (r *RepoMappings) Upsert(ctx context.Context, m RepoMapping) (RepoMapping, error) {
	var existing RepoMapping
	err := r.db.WithContext(ctx).Where("repository = ?", m.Repository).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
			return RepoMapping{}, fmt.Errorf("store: insert mapping: %w", err)
		}
		return m, nil
	case err != nil:
		return RepoMapping{}, fmt.Errorf("store: lookup mapping: %w", err)
	}

	existing.SlackChannel = m.SlackChannel
	existing.Mentions = m.Mentions
	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return RepoMapping{}, fmt.Errorf("store: update mapping: %w", err)
	}
	return existing, nil
}

// Get returns the mapping for repository, or ErrNotFound.
func (r *RepoMappings) Get(ctx context.Context, repository string) (RepoMapping, error) {
	var m RepoMapping
	err := r.db.WithContext(ctx).Where("repository = ?", repository).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RepoMapping{}, ErrNotFound
	}
	if err != nil {
		return RepoMapping{}, fmt.Errorf("store: get mapping: %w", err)
	}
	return m, nil
}

// List returns all mappings ordered by repository.
func (r *RepoMappings) List(ctx context.Context) ([]RepoMapping, error) {
	var ms []RepoMapping
	if err := r.db.WithContext(ctx).Order("repository ASC").Find(&ms).Error; err != nil {
		return nil, fmt.Errorf("store: list mappings: %w", err)
	}
	return ms, nil
}

// Delete removes the mapping for repository; returns ErrNotFound if nothing
// matched (caller can decide whether that's a CLI-level error).
func (r *RepoMappings) Delete(ctx context.Context, repository string) error {
	res := r.db.WithContext(ctx).Where("repository = ?", repository).Delete(&RepoMapping{})
	if res.Error != nil {
		return fmt.Errorf("store: delete mapping: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
