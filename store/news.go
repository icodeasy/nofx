package store

import (
	"fmt"
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
	Snippet     string    `json:"snippet" gorm:"type:text;nullable"`
	Language    string    `json:"language" gorm:"not null;default:'en'"` // 'en' for English, 'zh' for Chinese
	GoodCount   int       `json:"good_count" gorm:"not null;default:0"`  // Number of "good" feedbacks
	BadCount    int       `json:"bad_count" gorm:"not null;default:0"`   // Number of "bad" feedbacks
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// AIAnalysis represents an AI-generated market analysis
type AIAnalysis struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Timestamp int64     `json:"timestamp" gorm:"not null;unique"` // Unix timestamp key
	Date      string    `json:"date" gorm:"not null"`             // e.g., "January 15, 2026"
	Title     string    `json:"title" gorm:"type:text;nullable"`
	English   string    `json:"english" gorm:"type:text;nullable"`     // English analysis content
	Chinese   string    `json:"chinese" gorm:"type:text;nullable"`     // Chinese analysis content
	Language  string    `json:"language" gorm:"not null;default:'en'"` // 'en' or 'zh'
	Stars     int       `json:"stars" gorm:"default:0"`                // Star rating 0-5
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// NewsStore handles news-related database operations
type NewsStore struct {
	db *gorm.DB
}

// NormalizeAIAnalysisTimestamp rounds a timestamp down to the start of its 4-hour UTC bucket.
func NormalizeAIAnalysisTimestamp(timestamp int64) int64 {
	targetTime := time.Unix(timestamp, 0).UTC()
	bucketHour := (targetTime.Hour() / 4) * 4
	bucketTime := time.Date(targetTime.Year(), targetTime.Month(), targetTime.Day(), bucketHour, 0, 0, 0, time.UTC)
	return bucketTime.Unix()
}

// initTables initializes the news tables
func (ns *NewsStore) initTables() error {
	return ns.db.AutoMigrate(&NewsItem{}, &AIAnalysis{})
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

// List retrieves all marked news items with pagination (only items with feedback, ordered by published_at DESC)
// This is kept for potential future use, but main news loading now uses GetNewsInTimeRange
func (ns *NewsStore) List(limit, offset int, language string) ([]*NewsItem, int64, error) {
	var items []*NewsItem
	var total int64

	query := ns.db.Model(&NewsItem{})

	// Filter by language if specified
	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	// Only show news that has feedback (bull OR bear)
	query = query.Where("good_count = 1 OR bad_count = 1")

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
		Where("good_count = 1 OR bad_count = 1").
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

// UpdateFeedback creates news item with feedback if not exists, or updates existing (exclusive - sets one to 1, other to 0)
// This is the only way news gets saved to DB - when user gives feedback
func (ns *NewsStore) UpdateFeedback(newsID string, feedbackType string) error {
	if feedbackType != "good" && feedbackType != "bad" {
		return gorm.ErrInvalidValue
	}

	// Check if news item exists
	var existingItem NewsItem
	err := ns.db.Where("id = ?", newsID).First(&existingItem).Error

	if err == gorm.ErrRecordNotFound {
		// This shouldn't happen - frontend should call CreateWithFeedback instead
		return fmt.Errorf("news item not found - use CreateWithFeedback for new items")
	} else if err != nil {
		return err
	}

	// Update existing news item with exclusive feedback
	if feedbackType == "good" {
		return ns.db.Model(&NewsItem{}).
			Where("id = ?", newsID).
			Updates(map[string]interface{}{
				"good_count": 1,
				"bad_count":  0,
			}).Error
	} else {
		return ns.db.Model(&NewsItem{}).
			Where("id = ?", newsID).
			Updates(map[string]interface{}{
				"good_count": 0,
				"bad_count":  1,
			}).Error
	}
}

// CreateWithFeedback creates a new news item with feedback (used when user marks transient news)
func (ns *NewsStore) CreateWithFeedback(item *NewsItem, feedbackType string) error {
	if feedbackType != "good" && feedbackType != "bad" {
		return gorm.ErrInvalidValue
	}

	// Set feedback counts exclusively
	if feedbackType == "good" {
		item.GoodCount = 1
		item.BadCount = 0
	} else {
		item.GoodCount = 0
		item.BadCount = 1
	}

	// Check if news item already exists
	var existingItem NewsItem
	err := ns.db.Where("id = ?", item.ID).First(&existingItem).Error

	if err == nil {
		// Already exists, just update feedback
		return ns.UpdateFeedback(item.ID, feedbackType)
	} else if err != gorm.ErrRecordNotFound {
		return err
	}

	// Create new item with feedback
	return ns.db.Create(item).Error
}

// GetNewsInTimeRange retrieves news items within a time range with pagination
// Returns: only news within the time range (both marked and unmarked)
func (ns *NewsStore) GetNewsInTimeRange(startTime, endTime time.Time, language string, limit, offset int) ([]*NewsItem, int64, error) {
	var items []*NewsItem

	// Get items sorted by: marked news first (pinned), then by published_at DESC
	// Order: (good_count + bad_count) DESC puts marked (1+0 or 0+1=1) before unmarked (0+0=0)
	// Then within each group, sort by published_at DESC
	err := ns.db.Model(&NewsItem{}).
		Where("published_at >= ? AND published_at <= ?", startTime, endTime).
		Where(func(tx *gorm.DB) *gorm.DB {
			if language != "" && language != "all" {
				return tx.Where("language = ?", language)
			}
			return tx
		}(ns.db)).
		Order("(good_count + bad_count) DESC, published_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error

	if err != nil {
		return nil, 0, err
	}

	// Return items count as total (no separate COUNT query)
	return items, int64(len(items)), nil
}

// GetMarkedNewsInTimeRange retrieves AI-approved/user-marked news within a time range.
func (ns *NewsStore) GetMarkedNewsInTimeRange(startTime, endTime time.Time, language string, limit int) ([]*NewsItem, error) {
	var items []*NewsItem

	query := ns.db.Model(&NewsItem{}).
		Where("published_at >= ? AND published_at <= ?", startTime, endTime).
		Where("(good_count + bad_count) > 0")

	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	query = query.Order("published_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}

	return items, nil
}

// GetUnmarkedNewsInTimeRange retrieves raw RSS news that has not been marked yet.
func (ns *NewsStore) GetUnmarkedNewsInTimeRange(startTime, endTime time.Time, language string, limit int) ([]*NewsItem, error) {
	var items []*NewsItem

	query := ns.db.Model(&NewsItem{}).
		Where("published_at >= ? AND published_at <= ?", startTime, endTime).
		Where("(good_count + bad_count) = 0")

	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	query = query.Order("published_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}

	return items, nil
}

// GetAllURLsInTimeRange returns a map of all URLs in the given time range (for deduplication)
func (ns *NewsStore) GetAllURLsInTimeRange(startTime, endTime time.Time) (map[string]bool, error) {
	var urls []string

	err := ns.db.Model(&NewsItem{}).
		Where("published_at >= ? AND published_at <= ?", startTime, endTime).
		Pluck("url", &urls).Error

	if err != nil {
		return nil, err
	}

	urlMap := make(map[string]bool)
	for _, url := range urls {
		urlMap[url] = true
	}

	return urlMap, nil
}

// DeleteByIDs deletes news items by IDs.
func (ns *NewsStore) DeleteByIDs(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	return ns.db.Where("id IN ?", ids).Delete(&NewsItem{}).Error
}

// SaveAIAnalysis saves or updates an AI analysis (upsert based on timestamp)
func (ns *NewsStore) SaveAIAnalysis(analysis *AIAnalysis) error {
	return ns.db.Where("timestamp = ? AND language = ?", analysis.Timestamp, analysis.Language).
		Assign(analysis).
		FirstOrCreate(analysis).Error
}

// GetAIAnalysis retrieves an AI analysis by timestamp and language.
// It converts the timestamp to the start of the corresponding 4-hour UTC bucket.
func (ns *NewsStore) GetAIAnalysis(timestamp int64, language string) (*AIAnalysis, error) {
	var analysis AIAnalysis
	bucketTimestamp := NormalizeAIAnalysisTimestamp(timestamp)

	err := ns.db.Where("timestamp = ? AND language = ?", bucketTimestamp, language).First(&analysis).Error
	if err != nil {
		return nil, err
	}
	return &analysis, nil
}

// ListAIAnalyses retrieves all AI analyses ordered by timestamp DESC (newest first)
func (ns *NewsStore) ListAIAnalyses(language string, limit, offset int) ([]*AIAnalysis, int64, error) {
	var analyses []*AIAnalysis
	var total int64

	query := ns.db.Model(&AIAnalysis{})

	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated items, ordered by timestamp DESC
	err := ns.db.Model(&AIAnalysis{}).
		Where(func(tx *gorm.DB) *gorm.DB {
			if language != "" && language != "all" {
				return tx.Where("language = ?", language)
			}
			return tx
		}(ns.db)).
		Order("timestamp DESC").
		Limit(limit).
		Offset(offset).
		Find(&analyses).Error

	if err != nil {
		return nil, 0, err
	}

	return analyses, total, nil
}

// GetAllAIAnalyses retrieves all AI analyses for a language (no pagination)
func (ns *NewsStore) GetAllAIAnalyses(language string) ([]*AIAnalysis, error) {
	var analyses []*AIAnalysis

	query := ns.db.Model(&AIAnalysis{})
	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	err := query.Order("timestamp DESC").Find(&analyses).Error
	return analyses, err
}

// GetTopAIAnalyses retrieves a limited set of best analyses for reference selection.
func (ns *NewsStore) GetTopAIAnalyses(language string, limit int) ([]*AIAnalysis, error) {
	var analyses []*AIAnalysis
	if limit <= 0 {
		limit = 20
	}

	query := ns.db.Model(&AIAnalysis{})
	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	err := query.Order("stars DESC").Order("timestamp DESC").Limit(limit).Find(&analyses).Error
	return analyses, err
}

// GetLatestAIAnalysisByTitle retrieves the newest analysis for a title/language pair.
func (ns *NewsStore) GetLatestAIAnalysisByTitle(title, language string) (*AIAnalysis, error) {
	var analysis AIAnalysis

	query := ns.db.Model(&AIAnalysis{}).Where("title = ?", title)
	if language != "" && language != "all" {
		query = query.Where("language = ?", language)
	}

	if err := query.Order("timestamp DESC").First(&analysis).Error; err != nil {
		return nil, err
	}
	return &analysis, nil
}

// DeleteAIAnalysis deletes an AI analysis by timestamp
func (ns *NewsStore) DeleteAIAnalysis(timestamp int64) error {
	result := ns.db.Where("timestamp = ?", timestamp).Delete(&AIAnalysis{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateAIAnalysisStars updates the star rating for an AI analysis
func (ns *NewsStore) UpdateAIAnalysisStars(timestamp int64, stars int) error {
	// Validate stars is between 0 and 5
	if stars < 0 || stars > 5 {
		return fmt.Errorf("stars must be between 0 and 5")
	}

	result := ns.db.Model(&AIAnalysis{}).
		Where("timestamp = ?", timestamp).
		Update("stars", stars)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
