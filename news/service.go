package news

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"nofx/logger"
	"nofx/store"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service handles news fetching and storage
type Service struct {
	store      *store.Store
	httpClient *http.Client
	stopCh     chan struct{}
}

// NewService creates a new news service
func NewService(st *store.Store) *Service {
	return &Service{
		store: st,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

// GoogleNewsResult represents a parsed news item from Google News
type GoogleNewsResult struct {
	Title       string
	Source      string
	URL         string
	PublishedAt time.Time
	Snippet     string
}

// StartScheduler starts the daily news fetcher at 1 AM
func (s *Service) StartScheduler() {
	logger.Info("📰 News scheduler started (fetches daily at 1:00 AM)")

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		// Run once on startup to check if we need to fetch
		s.checkAndFetch()

		for {
			select {
			case <-ticker.C:
				s.checkAndFetch()
			case <-s.stopCh:
				logger.Info("📰 News scheduler stopped")
				return
			}
		}
	}()
}

// Stop stops the scheduler
func (s *Service) Stop() {
	close(s.stopCh)
}

// checkAndFetch checks if it's 1 AM and fetches news if needed
func (s *Service) checkAndFetch() {
	now := time.Now().UTC()

	// Check if it's 1 AM (within the same hour)
	if now.Hour() == 1 {
		// Check if we already fetched today
		lastFetch, err := s.store.News().GetLastFetchTime()
		if err == nil && lastFetch != nil {
			// If last fetch was today (in UTC), skip
			if lastFetch.Year() == now.Year() && lastFetch.YearDay() == now.YearDay() {
				logger.Infof("📰 News already fetched today, skipping")
				return
			}
		}

		logger.Info("📰 Starting daily news fetch...")
		if err := s.FetchAndStore(); err != nil {
			logger.Errorf("❌ Failed to fetch news: %v", err)
		} else {
			logger.Info("✅ News fetch completed successfully")
		}
	}
}

// FetchAndStore fetches news from Google News and stores it in the database
func (s *Service) FetchAndStore() error {
	// Google News RSS feed URL for blockchain news (last 24 hours)
	newsURL := "https://news.google.com/rss/search?q=blockchain%20when:1d&hl=en-US&gl=US&ceid=US:en"

	results, err := s.parseGoogleNewsRSS(newsURL)
	if err != nil {
		return fmt.Errorf("failed to parse Google News RSS: %w", err)
	}

	if len(results) == 0 {
		logger.Info("📰 No new articles found")
		return nil
	}

	// Filter out already existing URLs and prepare new items
	var newItems []*store.NewsItem
	addedCount := 0
	skippedCount := 0

	for _, result := range results {
		// Check if URL already exists
		exists, err := s.store.News().ExistsByURL(result.URL)
		if err != nil {
			logger.Warnf("⚠️ Failed to check if URL exists: %v", err)
			continue
		}

		if exists {
			skippedCount++
			continue
		}

		// Create new news item
		item := &store.NewsItem{
			ID:          uuid.New().String(),
			Title:       result.Title,
			Source:      result.Source,
			URL:         result.URL,
			PublishedAt: result.PublishedAt,
			Snippet:     result.Snippet,
		}

		newItems = append(newItems, item)
		addedCount++
	}

	// Store new items in batch
	if len(newItems) > 0 {
		if err := s.store.News().CreateBatch(newItems); err != nil {
			return fmt.Errorf("failed to store news items: %w", err)
		}
	}

	// Clean up old news (older than 30 days)
	deleted, err := s.store.News().DeleteOlderThan(30 * 24 * time.Hour)
	if err != nil {
		logger.Warnf("⚠️ Failed to delete old news: %v", err)
	} else if deleted > 0 {
		logger.Infof("📰 Cleaned up %d old news items", deleted)
	}

	logger.Infof("📰 Fetched %d new articles (skipped %d duplicates)", addedCount, skippedCount)

	return nil
}

// parseGoogleNewsRSS parses the Google News RSS feed and returns news items
func (s *Service) parseGoogleNewsRSS(feedURL string) ([]GoogleNewsResult, error) {
	resp, err := s.httpClient.Get(feedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch RSS feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse RSS XML feed
	var rss struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				PubDate     string `xml:"pubDate"`
				Description string `xml:"description"`
				Source      string `xml:"source"`
			} `xml:"item"`
		} `xml:"channel"`
	}

	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, fmt.Errorf("failed to parse RSS XML: %w", err)
	}

	var results []GoogleNewsResult
	for _, item := range rss.Channel.Items {
		// Parse publication date
		publishedAt := time.Now().UTC()
		if item.PubDate != "" {
			// Try common date formats
			formats := []string{
				time.RFC1123,
				time.RFC1123Z,
				"Mon, 2 Jan 2006 15:04:05 MST",
				"Mon, 02 Jan 2006 15:04:05 MST",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, strings.TrimSpace(item.PubDate)); err == nil {
					publishedAt = t
					break
				}
			}
		}

		// Clean up title (remove source suffix if present)
		title := strings.TrimSpace(item.Title)
		if parts := strings.Split(title, " - "); len(parts) > 1 {
			lastPart := strings.TrimSpace(parts[len(parts)-1])
			if len(lastPart) < 50 && !strings.Contains(lastPart, " ") {
				title = strings.Join(parts[:len(parts)-1], " - ")
			}
		}

		// Clean HTML from description
		description := cleanHTML(strings.TrimSpace(item.Description))

		result := GoogleNewsResult{
			Title:       cleanHTML(title),
			Source:      strings.TrimSpace(item.Source),
			URL:         strings.TrimSpace(item.Link),
			PublishedAt: publishedAt,
			Snippet:     description,
		}

		if result.URL != "" {
			results = append(results, result)
		}
	}

	logger.Infof("📰 Parsed %d articles from RSS feed", len(results))
	return results, nil
}

// cleanHTML removes HTML tags and decodes HTML entities
func cleanHTML(s string) string {
	// Remove HTML tags
	s = strings.ReplaceAll(s, "<br/>", " ")
	s = strings.ReplaceAll(s, "<br />", " ")
	s = strings.ReplaceAll(s, "<br>", " ")

	// Simple regex-free HTML tag removal
	for {
		start := strings.Index(s, "<")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], ">")
		if end == -1 {
			break
		}
		s = s[:start] + s[start+end+1:]
	}

	// Decode common HTML entities
	replacements := map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": "\"",
		"&#39;":  "'",
		"&nbsp;": " ",
		"&#x27;": "'",
		"&apos;": "'",
	}

	for entity, replacement := range replacements {
		s = strings.ReplaceAll(s, entity, replacement)
	}

	return strings.TrimSpace(s)
}

// FetchNewsManual manually triggers a news fetch (for testing or manual refresh)
func (s *Service) FetchNewsManual() error {
	logger.Info("📰 Manual news fetch triggered")
	return s.FetchAndStore()
}

// GetNews retrieves paginated news from the database
func (s *Service) GetNews(limit, offset int) ([]*store.NewsItem, int64, error) {
	return s.store.News().List(limit, offset)
}
