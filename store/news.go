package store

import (
	"time"

	"gorm.io/gorm"
)

// NewsItem represents a news article from Google News
type NewsItem struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	Title       string    `json:"title" gorm:"not null"`
	Source      string    `json:"source" gorm:"not null"`
	URL         string    `json:"url" gorm:"not null;unique"`
	PublishedAt time.Time `json:"published_at" gorm:"not null"`
	Snippet     string    `json:"snippet"`
	Language    string    `json:"language" gorm:"not null;default:'en'"` // 'en' for English, 'zh' for Chinese
	GoodCount   int       `json:"good_count" gorm:"not null;default:0"`  // Number of "good" feedbacks
	BadCount    int       `json:"bad_count" gorm:"not null;default:0"`   // Number of "bad" feedbacks
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// NewsStore handles news-related database operations
type NewsStore struct {
	db *gorm.DB
}

// initTables initializes the news tables
func (ns *NewsStore) initTables() error {
	return ns.db.AutoMigrate(&NewsItem{})
}

// Create creates a new news item
func (ns *NewsStore) Create(item *NewsItem) error {
	return ns.db.Create(item).Error
}

// CreateBatch creates multiple news items in a single transaction
func (ns *NewsStore) CreateBatch(items []*NewsItem) error {
	if len(items) == 0 {
		return nil
	}
	return ns.db.Create(&items).Error
}

// GetByID retrieves a news item by ID
func (ns *NewsStore) GetByID(id string) (*NewsItem, error) {
	var item NewsItem
	err := ns.db.Where("id = ?", id).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// List retrieves news items with pagination (ordered by published_at DESC)
func (ns *NewsStore) List(limit, offset int, language string) ([]*NewsItem, int64, error) {
	var items []*NewsItem
	var total int64

	query := ns.db.Model(&NewsItem{})

	// Filter by language if specified
	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	// Count total items
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated items, ordered by published_at DESC (newest first)
	err := ns.db.Model(&NewsItem{}).
		Where(func(tx *gorm.DB) *gorm.DB {
			if language != "" && language != "all" {
				return tx.Where("language = ?", language)
			}
			return tx
		}(ns.db)).
		Order("published_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error

	if err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// GetLatest retrieves the latest news item
func (ns *NewsStore) GetLatest() (*NewsItem, error) {
	var item NewsItem
	err := ns.db.Order("published_at DESC").First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// DeleteOlderThan deletes news items older than the specified duration
func (ns *NewsStore) DeleteOlderThan(duration time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-duration)
	result := ns.db.Where("published_at < ?", cutoff).Delete(&NewsItem{})
	return result.RowsAffected, result.Error
}

// ExistsByURL checks if a news item with the given URL already exists
func (ns *NewsStore) ExistsByURL(url string) (bool, error) {
	var count int64
	err := ns.db.Model(&NewsItem{}).Where("url = ?", url).Count(&count).Error
	return count > 0, err
}

// GetLastFetchTime returns the published_at of the most recently fetched news item
func (ns *NewsStore) GetLastFetchTime() (*time.Time, error) {
	var item NewsItem
	err := ns.db.Select("published_at").Order("published_at DESC").First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item.PublishedAt, nil
}

// UpdateFeedback updates the good/bad feedback count for a news item
func (ns *NewsStore) UpdateFeedback(newsID string, feedbackType string) error {
	var field string
	if feedbackType == "good" {
		field = "good_count"
	} else if feedbackType == "bad" {
		field = "bad_count"
	} else {
		return gorm.ErrInvalidValue
	}

	// Increment the appropriate counter
	return ns.db.Model(&NewsItem{}).
		Where("id = ?", newsID).
		UpdateColumn(field, gorm.Expr(field+" + ?", 1)).
		Error
}
