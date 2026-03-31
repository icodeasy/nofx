package news

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/store"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service handles news storage
type Service struct {
	store      *store.Store
	httpClient *http.Client
}

// NewService creates a new news service
func NewService(st *store.Store) *Service {
	return &Service{
		store: st,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GoogleNewsResult represents a parsed news item from Google News
type GoogleNewsResult struct {
	Title       string
	Source      string
	URL         string
	PublishedAt time.Time
	Snippet     string
	Language    string // 'en' for English, 'zh' for Chinese
	Direction   string
}

type rssQuerySet struct {
	en []string
	zh []string
}

const temporaryTradingOutlookPrefix = "Temporary Trading Outlook"
const temporaryTradingAnalysisTTL = 3 * time.Hour
const manualAnalysisAnchorKey = "news_manual_analysis_anchor_timestamp"
const autoAnalysisScanBucketKey = "news_auto_analysis_last_scan_bucket"
const autoFilteredNewsBatchSize = 20

type newsImpactAssessment struct {
	Index       int     `json:"index"`
	Keep        bool    `json:"keep"`
	Direction   string  `json:"direction"`
	Confidence  string  `json:"confidence"` // Changed from float64 to string to handle AI model responses
	Reason      string  `json:"reason"`
	DriverType  string  `json:"driver_type"`
	MarketScope string  `json:"market_scope"`
}

func defaultRSSQueries() rssQuerySet {
	return rssQuerySet{
		en: []string{
			"crypto",
			"bitcoin OR ethereum OR solana",
			"crypto ETF OR SEC OR regulation",
			"fed OR interest rates OR inflation crypto",
			"tariff OR war OR sanctions crypto",
			"stablecoin OR liquidity crypto",
		},
		zh: []string{
			"加密货币",
			"比特币 OR 以太坊 OR Solana",
			"加密 ETF OR SEC OR 监管",
			"美联储 OR 利率 OR 通胀 加密",
			"关税 OR 战争 OR 制裁 加密",
			"稳定币 OR 流动性 加密",
		},
	}
}

func (s *Service) buildRSSURLs(whenParam string, language string) []struct {
	Lang string
	URL  string
} {
	queries := defaultRSSQueries()
	urls := make([]struct {
		Lang string
		URL  string
	}, 0)

	addURLs := func(lang string, queryList []string, hl string, gl string, ceid string) {
		if language != "" && language != "all" && language != lang {
			return
		}
		for _, query := range queryList {
			urls = append(urls, struct {
				Lang string
				URL  string
			}{
				Lang: lang,
				URL:  fmt.Sprintf("https://news.google.com/rss/search?q=%s%%20when:%s&hl=%s&gl=%s&ceid=%s", urlEncodeQuery(query), whenParam, hl, gl, ceid),
			})
		}
	}

	addURLs("en", queries.en, "en-US", "US", "US:en")
	addURLs("zh", queries.zh, "zh-CN", "CN", "CN:zh-Hans")

	return urls
}

func urlEncodeQuery(query string) string {
	replacer := strings.NewReplacer(
		" ", "%20",
		"\"", "%22",
		"(", "%28",
		")", "%29",
		":", "%3A",
		"/", "%2F",
		"+", "%2B",
	)
	return replacer.Replace(query)
}

// FetchAndStore fetches news from Google News and stores it in the database
func (s *Service) FetchAndStore() error {
	newsURLs := s.buildRSSURLs("4h", "all")

	var allResults []GoogleNewsResult
	for _, item := range newsURLs {
		logger.Infof("📰 Fetching %s news from %s", item.Lang, item.URL)
		results, err := s.parseGoogleNewsRSS(item.URL, item.Lang)
		if err != nil {
			logger.Errorf("⚠️ Failed to parse %s Google News RSS: %v", item.Lang, err)
			continue
		}
		allResults = append(allResults, results...)
		logger.Infof("📰 Fetched %d %s articles", len(results), item.Lang)
	}

	if len(allResults) == 0 {
		logger.Info("📰 No new articles found")
		return nil
	}

	filteredResults, err := s.filterMarketMovingNews(allResults)
	if err != nil {
		return err
	}
	if len(filteredResults) == 0 {
		logger.Info("📰 No strong market-moving news found in this batch")
		return nil
	}

	// Filter out already existing URLs and prepare new items
	var newItems []*store.NewsItem
	addedCount := 0
	skippedCount := 0

	for _, result := range filteredResults {
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
			Language:    result.Language,
		}
		if strings.EqualFold(result.Direction, "bullish") {
			item.GoodCount = 1
		} else if strings.EqualFold(result.Direction, "bearish") {
			item.BadCount = 1
		}

		newItems = append(newItems, item)
		addedCount++
	}

	// Store new items in batch
	if len(newItems) > 0 {
		if err := s.store.News().CreateBatch(newItems); err != nil {
			// Handle UNIQUE constraint violations gracefully - it means some items were already stored
			if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "duplicate key") {
				logger.Warnf("⚠️ Some news items already exist (duplicate URLs): %v", err)
				// Try to store items one by one to avoid duplicates
				for _, item := range newItems {
					if err := s.store.News().Create(item); err != nil {
						if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "duplicate key") {
							skippedCount++
							logger.Debugf("Skipping duplicate URL: %s", item.URL)
						} else {
							logger.Warnf("⚠️ Failed to store news item: %v", err)
						}
					}
				}
			} else {
				return fmt.Errorf("failed to store news items: %w", err)
			}
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

func (s *Service) filterMarketMovingNews(results []GoogleNewsResult) ([]GoogleNewsResult, error) {
	candidateModels, err := CollectAvailableAIModels(s.store, "")
	if err != nil {
		return nil, err
	}

	filtered := make([]GoogleNewsResult, 0)
	for _, language := range []string{"en", "zh"} {
		group := make([]GoogleNewsResult, 0)
		for _, result := range results {
			if result.Language == language {
				group = append(group, result)
			}
		}
		for start := 0; start < len(group); start += autoFilteredNewsBatchSize {
			end := start + autoFilteredNewsBatchSize
			if end > len(group) {
				end = len(group)
			}
			kept, err := s.filterMarketMovingNewsBatch(candidateModels, group[start:end], language)
			if err != nil {
				logger.Warnf("⚠️ Failed to filter %s news batch: %v", language, err)
				continue
			}
			filtered = append(filtered, kept...)
		}
	}

	return filtered, nil
}

func (s *Service) filterMarketMovingNewsBatch(candidateModels []*store.AIModel, batch []GoogleNewsResult, language string) ([]GoogleNewsResult, error) {
	if len(batch) == 0 {
		return nil, nil
	}

	systemPrompt := "You are a crypto market news filter. Decide which headlines strongly affect near-term crypto market trend. Keep only strong, directional market movers. Return JSON only."
	userPrompt := buildNewsFilterPrompt(batch, language)
	response, _, err := CallWithAvailableAIModels(candidateModels, 4096, 45*time.Second, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	assessments, err := parseNewsImpactAssessments(response)
	if err != nil {
		return nil, err
	}

	kept := make([]GoogleNewsResult, 0)
	for _, assessment := range assessments {
		if assessment.Index < 0 || assessment.Index >= len(batch) {
			continue
		}

		// Parse confidence string to float64
		confidence, err := strconv.ParseFloat(assessment.Confidence, 64)
		if err != nil {
			logger.Warnf("⚠️ Failed to parse confidence value %q: %v", assessment.Confidence, err)
			continue
		}

		if !assessment.Keep || confidence < 0.7 {
			continue
		}
		if assessment.Direction != "bullish" && assessment.Direction != "bearish" {
			continue
		}
		item := batch[assessment.Index]
		item.Direction = assessment.Direction
		kept = append(kept, item)
	}

	return kept, nil
}

func buildNewsFilterPrompt(batch []GoogleNewsResult, language string) string {
	var sb strings.Builder
	sb.WriteString("Evaluate these news headlines for whether they strongly affect near-term crypto market trend.\n")
	sb.WriteString("Keep only headlines with strong likely impact on BTC or the broader crypto market over the next 4-24 hours.\n")
	sb.WriteString("Skip weak, repetitive, generic, promotional, or coin-specific noise unless it is clearly market-moving.\n")
	sb.WriteString("If kept, direction must be bullish or bearish.\n")
	sb.WriteString("Return JSON array only with fields: index, keep, direction, confidence, reason, driver_type, market_scope.\n")
	sb.WriteString("IMPORTANT: confidence must be a string number like \"0.8\" or \"0.95\", not a numeric value.\n")
	sb.WriteString("IMPORTANT: direction must be exactly \"bullish\" or \"bearish\".\n\n")
	sb.WriteString("Headlines:\n")
	for i, item := range batch {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s | %s | %s\n", i, item.PublishedAt.UTC().Format(time.RFC3339), item.Source, item.Title, truncatePromptText(item.Snippet, 240)))
	}
	if language == "zh" {
		sb.WriteString("\nUse English JSON values. direction must be bullish, bearish, or none.\n")
	}
	return sb.String()
}

func parseNewsImpactAssessments(response string) ([]newsImpactAssessment, error) {
	content := strings.TrimSpace(response)
	if strings.Contains(content, "```") {
		content = strings.ReplaceAll(content, "```json", "")
		content = strings.ReplaceAll(content, "```JSON", "")
		content = strings.ReplaceAll(content, "```", "")
		content = strings.TrimSpace(content)
	}

	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("failed to parse AI news filter response")
	}

	var assessments []newsImpactAssessment
	if err := json.Unmarshal([]byte(content[start:end+1]), &assessments); err != nil {
		return nil, err
	}
	return assessments, nil
}

// parseGoogleNewsRSS parses the Google News RSS feed and returns news items
func (s *Service) parseGoogleNewsRSS(feedURL string, lang string) ([]GoogleNewsResult, error) {
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
			Language:    lang,
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

// GetNews retrieves paginated news from the database
func (s *Service) GetNews(limit, offset int, language string) ([]*store.NewsItem, int64, error) {
	return s.store.News().List(limit, offset, language)
}

// SubmitFeedback submits feedback for a news item
func (s *Service) SubmitFeedback(newsID string, feedbackType string) error {
	return s.store.News().UpdateFeedback(newsID, feedbackType)
}

// CreateNewsWithFeedback creates a new news item with feedback (used when user marks transient news)
func (s *Service) CreateNewsWithFeedback(id, title, source, url string, publishedAt time.Time, snippet, language, feedbackType string) error {
	item := &store.NewsItem{
		ID:          id,
		Title:       title,
		Source:      source,
		URL:         url,
		PublishedAt: publishedAt,
		Snippet:     snippet,
		Language:    language,
	}
	return s.store.News().CreateWithFeedback(item, feedbackType)
}

// GetNewsInTimeRange retrieves news within a time range with pagination
// It only fetches from DB (news is fetched periodically by scheduler)
func (s *Service) GetNewsInTimeRange(startTime, endTime time.Time, language string, limit, offset int) ([]*store.NewsItem, int64, error) {
	// Only fetch from DB - news is fetched periodically by scheduler
	dbItems, total, err := s.store.News().GetNewsInTimeRange(startTime, endTime, language, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return dbItems, total, nil
}

// GetCuratedNewsInTimeRange returns AI-curated nearby news, topping up from raw RSS-backed news when needed.
// GetCuratedNewsInTimeRange returns manually marked news in the time range
// Note: Without RSS fetching, this will return empty unless news is manually added via API
func (s *Service) GetCuratedNewsInTimeRange(
	startTime, endTime time.Time,
	language string,
	minMarked int,
	rawLimit int,
	aiClient mcp.AIClient,
	purpose string,
	contextHint string,
) ([]*store.NewsItem, error) {
	// Simply return manually marked news (bullish/bearish marked by users)
	// No AI filtering since there's no automatic news source
	marked, err := s.store.News().GetMarkedNewsInTimeRange(startTime, endTime, language, minMarked)
	if err != nil {
		return nil, err
	}
	return marked, nil
}

// RecordManualAnalysisAnchor stores the last human-selected anchor timestamp for auto analysis scanning.
func (s *Service) RecordManualAnalysisAnchor(timestamp int64) error {
	if timestamp <= 0 {
		return nil
	}
	return s.store.SetSystemConfig(manualAnalysisAnchorKey, strconv.FormatInt(timestamp, 10))
}

// MaybeGenerateAutoAnalysisFromAnchor scans 4h structure from the last human anchor and auto-generates
// an analysis once a top/bottom pair has formed.
func (s *Service) MaybeGenerateAutoAnalysisFromAnchor(now time.Time) error {
	now = now.UTC()
	currentBucket := store.NormalizeAIAnalysisTimestamp(now.Unix())

	lastBucketStr, err := s.store.GetSystemConfig(autoAnalysisScanBucketKey)
	if err == nil && lastBucketStr != "" {
		if lastBucket, parseErr := strconv.ParseInt(lastBucketStr, 10, 64); parseErr == nil && lastBucket == currentBucket {
			return nil
		}
	}
	if err := s.store.SetSystemConfig(autoAnalysisScanBucketKey, strconv.FormatInt(currentBucket, 10)); err != nil {
		logger.Warnf("⚠️ Failed to update auto analysis scan bucket: %v", err)
	}

	anchorStr, err := s.store.GetSystemConfig(manualAnalysisAnchorKey)
	if err != nil || anchorStr == "" {
		return err
	}

	anchorTimestamp, err := strconv.ParseInt(anchorStr, 10, 64)
	if err != nil || anchorTimestamp <= 0 {
		return err
	}

	anchorTime := time.Unix(anchorTimestamp, 0).UTC()
	if !now.After(anchorTime) {
		return nil
	}

	startPivot, endPivot, err := s.findAutoAnalysisPivotPair(anchorTime, now)
	if err != nil {
		return err
	}
	if startPivot == nil || endPivot == nil {
		return nil
	}

	for _, language := range []string{"en", "zh"} {
		if _, err := s.GenerateRangeMarketAnalysis(
			startPivot.Time.Unix(),
			startPivot.Time.Unix(),
			startPivot.Price,
			endPivot.Time.Unix(),
			endPivot.Price,
			endPivot.Type,
			"BTCUSDT",
			"",
			language,
			false,
		); err != nil {
			logger.Warnf("⚠️ Failed to auto-generate %s range analysis: %v", language, err)
		}
	}

	return nil
}

// GenerateTemporaryTradingAnalysis builds a fresh predictive analysis for the current trading cycle.
func (s *Service) GenerateTemporaryTradingAnalysis(
	now time.Time,
	language string,
	aiClient mcp.AIClient,
	symbols []string,
) (*store.AIAnalysis, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is required")
	}

	normalizedSymbols := normalizeSymbols(symbols)
	cacheTitle := buildTemporaryTradingAnalysisTitle(normalizedSymbols)
	cachedAnalysis, err := s.store.News().GetLatestAIAnalysisByTitle(cacheTitle, language)
	if err == nil && cachedAnalysis != nil {
		cachedAt := time.Unix(cachedAnalysis.Timestamp, 0).UTC()
		if now.Sub(cachedAt) <= temporaryTradingAnalysisTTL {
			logger.Infof("📰 Reusing cached temporary trading analysis from %s", cachedAt.Format(time.RFC3339))
			return cachedAnalysis, nil
		}
	}

	startTime := now.Add(-36 * time.Hour)
	contextHint := "Focus on predicting the likely near-term market trend."
	if len(normalizedSymbols) > 0 {
		contextHint += " Relevant symbols/themes: " + strings.Join(normalizedSymbols, ", ")
	}

	curatedNews, err := s.GetCuratedNewsInTimeRange(
		startTime,
		now,
		language,
		6,
		15,
		aiClient,
		"startup predictive trading analysis",
		contextHint,
	)
	if err != nil {
		return nil, err
	}

	historicalExamples, err := s.store.News().GetTopAIAnalyses(language, 20)
	if err != nil {
		logger.Warnf("⚠️ Failed to load historical analyses for examples: %v", err)
	}
	historicalExamples = selectReferenceAnalyses(historicalExamples)

	systemPrompt := "You are a trading-oriented crypto market analyst. Use curated recent news to predict the likely current trend. Historical analyses are examples only, not facts."
	userPrompt := buildTemporaryTradingAnalysisPrompt(now, normalizedSymbols, curatedNews, historicalExamples)

	response, err := aiClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	english, chinese := parseBilingualMarkdown(response)
	analysis := &store.AIAnalysis{
		Timestamp: now.Unix(),
		Date:      now.UTC().Format("January 2, 2006"),
		Title:     cacheTitle,
		English:   english,
		Chinese:   chinese,
		Language:  language,
	}
	if err := s.store.News().SaveAIAnalysis(analysis); err != nil {
		logger.Warnf("⚠️ Failed to save temporary trading analysis cache: %v", err)
	}
	return analysis, nil
}

type autoPivot struct {
	Type  string
	Time  time.Time
	Price float64
}

func (s *Service) findAutoAnalysisPivotPair(anchorTime, now time.Time) (*autoPivot, *autoPivot, error) {
	klines, err := market.GetKlinesRange("BTCUSDT", "4h", anchorTime, now)
	if err != nil {
		return nil, nil, err
	}
	if len(klines) < 3 {
		return nil, nil, nil
	}

	tops, bottoms := market.FindTopsAndBottoms(klines, 1.06)
	pivots := make([]autoPivot, 0, len(tops)+len(bottoms))

	for _, top := range tops {
		if top.Time <= anchorTime.UnixMilli() {
			continue
		}
		pivots = append(pivots, autoPivot{
			Type:  "top",
			Time:  time.UnixMilli(top.Time).UTC(),
			Price: top.Price,
		})
	}
	for _, bottom := range bottoms {
		if bottom.Time <= anchorTime.UnixMilli() {
			continue
		}
		pivots = append(pivots, autoPivot{
			Type:  "bottom",
			Time:  time.UnixMilli(bottom.Time).UTC(),
			Price: bottom.Price,
		})
	}

	if len(pivots) < 2 {
		return nil, nil, nil
	}

	sort.Slice(pivots, func(i, j int) bool {
		return pivots[i].Time.Before(pivots[j].Time)
	})

	start := pivots[0]
	for i := 1; i < len(pivots); i++ {
		if pivots[i].Type != start.Type {
			end := pivots[i]
			return &start, &end, nil
		}
	}

	return nil, nil, nil
}

// GenerateRangeMarketAnalysis reuses the click-to-pivot analysis flow for a known range.
func (s *Service) GenerateRangeMarketAnalysis(
	timestamp int64,
	clickTimestamp int64,
	priceAtClick float64,
	pivotTimestamp int64,
	pivotPrice float64,
	pivotType string,
	symbol string,
	userID string,
	language string,
	forceRefresh bool,
) (*store.AIAnalysis, error) {
	clickTime := time.Unix(clickTimestamp, 0).UTC()
	pivotTime := time.Unix(pivotTimestamp, 0).UTC()
	duration := pivotTime.Sub(clickTime)
	if duration < 0 {
		duration = -duration
	}
	hoursDiff := int(duration.Hours())
	durationLabel := fmt.Sprintf("%d hours", hoursDiff)
	if hoursDiff >= 48 {
		durationLabel = fmt.Sprintf("%d days", hoursDiff/24)
	}

	pctChange := 0.0
	pctChangeStr := ""
	if priceAtClick > 0 {
		pctChange = ((pivotPrice - priceAtClick) / priceAtClick) * 100
		pctChangeStr = fmt.Sprintf("%.1f%%", pctChange)
		if pctChange > 0 {
			pctChangeStr += " (rise)"
		} else if pctChange < 0 {
			pctChangeStr += " (drop)"
		}
	}

	normalizedSymbol := strings.TrimSpace(strings.ToUpper(symbol))
	title := "4H Market Analysis"
	if normalizedSymbol != "" {
		title += " (" + normalizedSymbol + ")"
	}
	title += ": "
	title += "from " + clickTime.UTC().Format("2006-01-02 15:04 UTC") + " $" + fmt.Sprintf("%.2f", priceAtClick)
	title += " to " + pivotTime.UTC().Format("2006-01-02 15:04 UTC") + " $" + fmt.Sprintf("%.2f", pivotPrice)
	if durationLabel != "" {
		title += ", " + durationLabel
	}
	if pctChangeStr != "" {
		title += ", price " + pctChangeStr
	}

	analysisTimestamp := store.NormalizeAIAnalysisTimestamp(clickTimestamp)
	dateStr := time.Unix(analysisTimestamp, 0).UTC().Format("January 2, 2006 15:04 UTC")

	if !forceRefresh {
		existingAnalysis, err := s.store.News().GetAIAnalysis(analysisTimestamp, language)
		if err == nil && existingAnalysis != nil {
			// Range analyses are bucketed to 4 hours, so only reuse the cache when
			// the exact click-to-pivot range matches the stored title.
			if strings.TrimSpace(existingAnalysis.Title) == title {
				return existingAnalysis, nil
			}
		}
	}

	technicalContext := "\n\n## " + title + "\n\n"
	if normalizedSymbol != "" {
		technicalContext += "**Symbol:** " + normalizedSymbol + "\n\n"
	}
	if pctChange != 0 {
		trendDirection := "BEARISH (Price falling trend)"
		trendExplanation := "The price is falling from the click point toward the next BOTTOM (support). Analyze what news or factors likely drove this downward move into support."
		if pctChange > 0 {
			trendDirection = "BULLISH (Price rising trend)"
			if pivotType == "top" {
				trendExplanation = "The price is rising from the click point toward the next TOP (resistance). Analyze what news or factors likely drove this upward move into resistance."
			} else {
				trendExplanation = "The price is rising from the click point toward the next BOTTOM confirmation area. Analyze what news or factors likely supported this upward move."
			}
		} else if pivotType == "top" {
			trendExplanation = "The price is falling from the click point toward the next TOP confirmation area. Analyze what news or factors likely drove this downward move."
		}
		technicalContext += "**Observed Price Movement:** " + trendDirection + "\n\n"
		technicalContext += "**Investigation Context:** " + trendExplanation + "\n\n"
	}
	technicalContext += "**Your Investigation Task:**\n"
	technicalContext += "1. Search for and identify relevant news or events that occurred after the click point and before the next pivot\n"
	technicalContext += "2. Assess the timing: Did events occur early enough to influence the move from the click point to the next pivot?\n"
	technicalContext += "3. Explain how identified events contributed to the subsequent price movement\n"
	technicalContext += "4. If no significant news explains the move, explicitly state that it appears technically driven\n\n"

	candidateModels, err := CollectAvailableAIModels(s.store, userID)
	if err != nil {
		return nil, err
	}

	systemPrompt := "You are a specialized crypto market analyst. Search for relevant news and use technical context to explain the observed market move. Return bilingual markdown."
	userPrompt := buildRangeAnalysisPrompt(dateStr, normalizedSymbol, technicalContext)

	response, _, err := CallWithAvailableAIModels(candidateModels, 8192, 60*time.Second, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	english, chinese := parseBilingualMarkdown(response)
	analysis := &store.AIAnalysis{
		Timestamp: analysisTimestamp,
		Date:      dateStr,
		Title:     title,
		English:   english,
		Chinese:   chinese,
		Language:  language,
		Stars:     0,
	}
	if err := s.store.News().SaveAIAnalysis(analysis); err != nil {
		logger.Warnf("⚠️ Failed to save range analysis: %v", err)
	}
	return analysis, nil
}

func buildRangeAnalysisPrompt(dateStr string, symbol string, technicalContext string) string {
	intro := []string{
		"Analyze the market context for the 4-hour trading range anchored at " + dateStr + ".",
		"Use technical context plus relevant recent events to explain the move in a way that is useful for a 4-hour strategy.",
	}
	if symbol != "" {
		intro = append(intro, "Focus specifically on the "+symbol+" market and keep the explanation tied to that symbol.")
	}

	return BuildMarketAnalysisPrompt(
		"4-Hour Market Analysis",
		intro,
		technicalContext,
	)
}

// calculateWhenParam determines the optimal Google News "when:" parameter based on duration
// Returns: "1h", "1d", "1w", or "1m"
func (s *Service) calculateWhenParam(duration time.Duration) string {
	hours := duration.Hours()

	switch {
	case hours <= 1:
		return "1h"
	case hours <= 24:
		return "1h"
	case hours <= 168: // 7 days
		return "1d"
	case hours <= 720: // 30 days
		return "1w"
	default:
		return "1m"
	}
}

// fetchRSSForTimeRange fetches fresh RSS news and filters by time range
func (s *Service) fetchRSSForTimeRange(startTime, endTime time.Time, language string) ([]*store.NewsItem, error) {
	// Calculate the time range duration
	duration := endTime.Sub(startTime)

	// Determine the optimal "when:" parameter based on range
	// Google News RSS supports: 1h, 1d, 1w, 1m
	whenParam := s.calculateWhenParam(duration)

	logger.Infof("📰 Fetching RSS with when:%s (range: %.1f hours)", whenParam, duration.Hours())

	newsURLs := s.buildRSSURLs(whenParam, language)

	var allResults []GoogleNewsResult
	for _, item := range newsURLs {
		results, err := s.parseGoogleNewsRSS(item.URL, item.Lang)
		if err != nil {
			logger.Warnf("⚠️ Failed to parse %s Google News RSS: %v", item.Lang, err)
			continue
		}
		allResults = append(allResults, results...)
	}

	if len(allResults) == 0 {
		return nil, nil
	}

	// Get existing URLs from DB to avoid duplicates
	existingURLs, err := s.store.News().GetAllURLsInTimeRange(startTime, endTime)
	if err != nil {
		logger.Warnf("⚠️ Failed to get existing URLs: %v", err)
		existingURLs = make(map[string]bool)
	}

	// Filter by time range and convert to store items
	var items []*store.NewsItem
	for _, result := range allResults {
		// Check if within time range
		if result.PublishedAt.Before(startTime) || result.PublishedAt.After(endTime) {
			continue
		}

		// Skip if already exists in DB
		if existingURLs[result.URL] {
			continue
		}

		// Generate ID from URL hash
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(result.URL)).String()

		item := &store.NewsItem{
			ID:          id,
			Title:       result.Title,
			Source:      result.Source,
			URL:         result.URL,
			PublishedAt: result.PublishedAt,
			Snippet:     result.Snippet,
			Language:    result.Language,
		}
		items = append(items, item)
	}

	return items, nil
}

func buildTemporaryTradingAnalysisPrompt(
	now time.Time,
	symbols []string,
	curatedNews []*store.NewsItem,
	historicalExamples []*store.AIAnalysis,
) string {
	var sb strings.Builder
	sb.WriteString("Task: Predict the likely current market trend for trading use.\n")
	sb.WriteString("Infer likely near-term market direction from the curated recent news/events.\n")
	sb.WriteString("Use historical analyses only as high-quality reference examples for reasoning patterns, not as current facts.\n")
	if len(symbols) > 0 {
		sb.WriteString("Current focus symbols/themes: " + strings.Join(symbols, ", ") + "\n")
	}
	sb.WriteString("Current time: " + now.UTC().Format(time.RFC3339) + "\n\n")
	if len(curatedNews) == 0 {
		sb.WriteString("Curated recent news: none available.\n")
		sb.WriteString("If news support is weak, say so clearly and lean on uncertainty rather than inventing conviction.\n\n")
	} else {
		sb.WriteString("Curated recent news:\n")
		for _, item := range curatedNews {
			sentiment := "mixed"
			if item.GoodCount > 0 {
				sentiment = "bullish"
			} else if item.BadCount > 0 {
				sentiment = "bearish"
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s | %s | %s | %s\n",
				sentiment,
				item.PublishedAt.UTC().Format(time.RFC3339),
				item.Source,
				item.Title,
				item.Snippet,
			))
		}
		sb.WriteString("\n")
	}

	if len(historicalExamples) > 0 {
		sb.WriteString("Historical analysis examples (reference only, prefer starred quality patterns):\n")
		exampleIndex := 0
		for _, example := range historicalExamples {
			content := strings.TrimSpace(example.English)
			if content == "" {
				continue
			}
			content = truncatePromptText(content, 800)
			header := formatHistoricalExampleHeader(example)
			if example.Stars > 0 {
				header += fmt.Sprintf(" | stars=%d/5", example.Stars)
			}
			exampleIndex++
			sb.WriteString(fmt.Sprintf("Example %d: %s\n%s\n\n", exampleIndex, header, content))
		}
	}

	sb.WriteString("Output a predictive trading-oriented analysis with:\n")
	sb.WriteString("1. News / event drivers: the main recent events or explicitly say none\n")
	sb.WriteString("2. Driver type: macro, regulation, institutional, crypto-native, sentiment, liquidity, technical, mixed, or none\n")
	sb.WriteString("3. Trend prediction for the next 4 hours: bullish, bearish, neutral, mixed, or high-uncertainty\n")
	sb.WriteString("4. Reasoning: how the identified drivers support that prediction\n")
	sb.WriteString("5. Confidence: low, medium, or high\n")
	sb.WriteString("6. Risk / invalidation: what could quickly invalidate the view\n")
	sb.WriteString("7. News weight: how much the strategy should rely on this news backdrop\n\n")
	sb.WriteString("Please provide the response in English using markdown with this structure:\n")
	sb.WriteString("### English\n")
	sb.WriteString("**News / Event Drivers**:\n- ...\n\n")
	sb.WriteString("**Driver Type**: ...\n\n")
	sb.WriteString("**Trend Prediction (Next 4H)**: ...\n\n")
	sb.WriteString("**Reasoning**: ...\n\n")
	sb.WriteString("**Confidence**: ...\n\n")
	sb.WriteString("**Risk / Invalidation**:\n- ...\n\n")
	sb.WriteString("**News Weight For Strategy**: low / medium / high\n")
	return sb.String()
}

func truncatePromptText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func parseBilingualMarkdown(response string) (string, string) {
	englishStart := -1
	englishEnd := -1
	chineseStart := -1
	chineseEnd := -1

	lines := strings.Split(response, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "###") {
			if strings.Contains(lower, "english") {
				englishStart = i + 1
				if chineseStart > 0 && chineseEnd == -1 {
					chineseEnd = i
				}
			} else if strings.Contains(lower, "中文") || strings.Contains(lower, "chinese") {
				chineseStart = i + 1
				if englishStart > 0 && englishEnd == -1 {
					englishEnd = i
				}
			}
		}
	}

	if englishStart == -1 {
		return strings.TrimSpace(response), strings.TrimSpace(response)
	}

	if englishEnd == -1 {
		englishEnd = len(lines)
	}
	if chineseEnd == -1 {
		chineseEnd = len(lines)
	}

	english := strings.TrimSpace(strings.Join(lines[englishStart:englishEnd], "\n"))
	chinese := ""
	if chineseStart >= 0 && chineseStart <= len(lines) {
		chinese = strings.TrimSpace(strings.Join(lines[chineseStart:chineseEnd], "\n"))
	}
	if english == "" {
		english = strings.TrimSpace(response)
	}
	if chinese == "" {
		chinese = english
	}
	return english, chinese
}

func formatHistoricalExampleHeader(example *store.AIAnalysis) string {
	if example == nil {
		return "historical analysis"
	}

	parts := []string{}
	if example.Title != "" {
		parts = append(parts, example.Title)
	}
	if example.Date != "" {
		parts = append(parts, example.Date)
	}
	if len(parts) == 0 {
		return "historical analysis"
	}
	return strings.Join(parts, " | ")
}

func selectReferenceAnalyses(analyses []*store.AIAnalysis) []*store.AIAnalysis {
	filtered := make([]*store.AIAnalysis, 0, len(analyses))
	for _, analysis := range analyses {
		if analysis == nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(analysis.Title), temporaryTradingOutlookPrefix) {
			continue
		}
		filtered = append(filtered, analysis)
	}

	if len(filtered) <= 2 {
		return filtered
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Stars != filtered[j].Stars {
			return filtered[i].Stars > filtered[j].Stars
		}
		return filtered[i].Timestamp > filtered[j].Timestamp
	})

	var bullish *store.AIAnalysis
	var bearish *store.AIAnalysis
	fallback := make([]*store.AIAnalysis, 0, 2)

	for _, analysis := range filtered {
		if analysis == nil {
			continue
		}
		if len(fallback) < 2 {
			fallback = append(fallback, analysis)
		}

		switch inferAnalysisBias(analysis) {
		case "bullish":
			if bullish == nil {
				bullish = analysis
			}
		case "bearish":
			if bearish == nil {
				bearish = analysis
			}
		}

		if bullish != nil && bearish != nil {
			break
		}
	}

	selected := make([]*store.AIAnalysis, 0, 2)
	if bullish != nil {
		selected = append(selected, bullish)
	}
	if bearish != nil && bearish != bullish {
		selected = append(selected, bearish)
	}

	if len(selected) == 0 {
		return fallback
	}
	if len(selected) == 1 {
		for _, analysis := range filtered {
			if analysis != nil && analysis != selected[0] {
				selected = append(selected, analysis)
				break
			}
		}
	}
	if len(selected) > 2 {
		selected = selected[:2]
	}
	return selected
}

func inferAnalysisBias(analysis *store.AIAnalysis) string {
	if analysis == nil {
		return ""
	}

	text := strings.ToLower(strings.TrimSpace(
		strings.Join([]string{analysis.Title, analysis.English, analysis.Chinese}, " "),
	))
	if text == "" {
		return ""
	}

	bullishKeywords := []string{
		"bullish", "uptrend", "upward", "breakout", "support", "rebound", "recovery",
		"上涨", "走强", "突破", "反弹", "回升", "支撑",
	}
	bearishKeywords := []string{
		"bearish", "downtrend", "downward", "breakdown", "rejection", "risk-off", "selloff",
		"下跌", "走弱", "跌破", "回落", "风险", "抛售", "承压",
	}

	bullishScore := keywordScore(text, bullishKeywords)
	bearishScore := keywordScore(text, bearishKeywords)

	switch {
	case bullishScore > bearishScore:
		return "bullish"
	case bearishScore > bullishScore:
		return "bearish"
	default:
		return ""
	}
}

func keywordScore(text string, keywords []string) int {
	score := 0
	for _, keyword := range keywords {
		score += strings.Count(text, keyword)
	}
	return score
}

func normalizeSymbols(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(symbols))
	normalized := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(strings.ToUpper(symbol))
		if symbol == "" || seen[symbol] {
			continue
		}
		seen[symbol] = true
		normalized = append(normalized, symbol)
	}
	sort.Strings(normalized)
	return normalized
}

func buildTemporaryTradingAnalysisTitle(symbols []string) string {
	if len(symbols) == 0 {
		return temporaryTradingOutlookPrefix + " | MARKET"
	}
	return temporaryTradingOutlookPrefix + " | " + strings.Join(symbols, ",")
}
