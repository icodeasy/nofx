package api

import (
	"fmt"
	"net/http"
	"nofx/logger"
	"nofx/news"
	"nofx/store"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const noFuturePivotError = "No next top or bottom has formed after the selected timestamp"
const defaultNewsAnalysisSymbol = "BTCUSDT"

// NewsHandler handles news-related API requests
type NewsHandler struct {
	newsService *news.Service
	store       *store.Store
}

// NewNewsHandler creates a new news handler
func NewNewsHandler(newsService *news.Service, st *store.Store) *NewsHandler {
	return &NewsHandler{
		newsService: newsService,
		store:       st,
	}
}

// GetNewsRequest represents the query parameters for getting news
type GetNewsRequest struct {
	Limit  int `form:"limit" binding:"required,min=1,max=100"`
	Offset int `form:"offset" binding:"required,min=0"`
}

// NewsResponse represents the API response for news
type NewsResponse struct {
	News  []*NewsItemResponse `json:"news"`
	Total int64               `json:"total"`
}

// NewsItemResponse represents a single news item in the API response
type NewsItemResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Source      string `json:"source"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
	Snippet     string `json:"snippet"`
	GoodCount   int    `json:"good_count"`
	BadCount    int    `json:"bad_count"`
}

// HandleGetNews handles GET /api/news - retrieve paginated news
func (h *NewsHandler) HandleGetNews(c *gin.Context) {
	// Parse query parameters with defaults
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")
	language := c.DefaultQuery("language", "all") // "en", "zh", or "all"

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Fetch news from service
	items, total, err := h.newsService.GetNews(limit, offset, language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch news",
		})
		return
	}

	// Convert to response format
	response := NewsResponse{
		News:  make([]*NewsItemResponse, 0, len(items)),
		Total: total,
	}

	for _, item := range items {
		response.News = append(response.News, &NewsItemResponse{
			ID:          item.ID,
			Title:       item.Title,
			Source:      item.Source,
			URL:         item.URL,
			PublishedAt: item.PublishedAt.Format("2006-01-02T15:04:05Z"),
			Snippet:     item.Snippet,
			GoodCount:   item.GoodCount,
			BadCount:    item.BadCount,
		})
	}

	c.JSON(http.StatusOK, response)
}

// HandleNewsFeedback handles POST /api/news/:id/feedback - submit feedback for a news item
// Accepts full news item data to save if it doesn't exist in DB yet (from transient chart click)
func (h *NewsHandler) HandleNewsFeedback(c *gin.Context) {
	newsID := c.Param("id")

	var req struct {
		Type        string `json:"type" binding:"required,oneof=good bad"`
		Title       string `json:"title"`
		Source      string `json:"source"`
		URL         string `json:"url"`
		PublishedAt string `json:"published_at"`
		Snippet     string `json:"snippet"`
		Language    string `json:"language"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid feedback type. Must be 'good' or 'bad'",
		})
		return
	}

	// If full news data is provided, create with feedback (for transient news from chart clicks)
	if req.Title != "" && req.URL != "" {
		// Parse published_at
		publishedAt := time.Now().UTC()
		if req.PublishedAt != "" {
			if parsed, err := time.Parse("2006-01-02T15:04:05Z", req.PublishedAt); err == nil {
				publishedAt = parsed
			}
		}

		// Create news item with feedback
		if err := h.newsService.CreateNewsWithFeedback(newsID, req.Title, req.Source, req.URL, publishedAt, req.Snippet, req.Language, req.Type); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to submit feedback",
			})
			return
		}
	} else {
		// Update existing news item feedback only
		if err := h.newsService.SubmitFeedback(newsID, req.Type); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to submit feedback",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Feedback submitted successfully",
	})
}

// HandleGetNewsAroundTimestamp handles GET /api/news/around - retrieve news around a timestamp
func (h *NewsHandler) HandleGetNewsAroundTimestamp(c *gin.Context) {
	timestampStr := c.Query("timestamp")
	if timestampStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp parameter is required"})
		return
	}

	// Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timestamp format"})
		return
	}

	// Get before/after hours (default 24 hours each)
	beforeHours := 24
	if beforeStr := c.Query("before_hours"); beforeStr != "" {
		if parsed, err := strconv.Atoi(beforeStr); err == nil && parsed >= 0 && parsed <= 168 {
			beforeHours = parsed
		}
	}

	afterHours := 24
	if afterStr := c.Query("after_hours"); afterStr != "" {
		if parsed, err := strconv.Atoi(afterStr); err == nil && parsed >= 0 && parsed <= 168 {
			afterHours = parsed
		}
	}

	language := c.DefaultQuery("language", "all")

	// Get pagination parameters
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		limit = 100
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Calculate time range
	targetTime := time.Unix(timestamp, 0)
	startTime := targetTime.Add(-time.Duration(beforeHours) * time.Hour)
	endTime := targetTime.Add(time.Duration(afterHours) * time.Hour)

	// Fetch news in time range
	items, total, err := h.newsService.GetNewsInTimeRange(startTime, endTime, language, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch news"})
		return
	}

	// Convert to response format
	response := NewsResponse{
		News:  make([]*NewsItemResponse, 0, len(items)),
		Total: total,
	}

	for _, item := range items {
		response.News = append(response.News, &NewsItemResponse{
			ID:          item.ID,
			Title:       item.Title,
			Source:      item.Source,
			URL:         item.URL,
			PublishedAt: item.PublishedAt.Format("2006-01-02T15:04:05Z"),
			Snippet:     item.Snippet,
			GoodCount:   item.GoodCount,
			BadCount:    item.BadCount,
		})
	}

	c.JSON(http.StatusOK, response)
}

// AIAnalysisRequest represents the query parameters for AI analysis
type AIAnalysisRequest struct {
	Timestamp int64  `form:"timestamp" binding:"required"`
	Language  string `form:"language"`
}

// AIAnalysisResponse represents the API response for AI analysis
type AIAnalysisResponse struct {
	Timestamp int64  `json:"timestamp"`
	Title     string `json:"title,omitempty"`
	English   string `json:"english"`
	Chinese   string `json:"chinese"`
	Date      string `json:"date"` // e.g., "January 15, 2026"
	Stars     int    `json:"stars"`
}

type rangeAnalysisContext struct {
	clickTimestamp int64
	clickPrice     float64
	pivotTimestamp int64
	pivotPrice     float64
	pivotType      string
}

func parseRangeAnalysisContext(
	timestampAtClick string,
	priceAtClick string,
	nearestTopPrice string,
	nearestTopTime string,
	nearestBottomPrice string,
	nearestBottomTime string,
	nearestPivotType string,
) (*rangeAnalysisContext, error) {
	if timestampAtClick == "" || priceAtClick == "" || nearestPivotType == "" {
		return nil, nil
	}

	clickTimestamp, err := strconv.ParseInt(timestampAtClick, 10, 64)
	if err != nil {
		return nil, nil
	}

	clickPrice, err := strconv.ParseFloat(priceAtClick, 64)
	if err != nil {
		return nil, nil
	}

	ctx := &rangeAnalysisContext{
		clickTimestamp: clickTimestamp,
		clickPrice:     clickPrice,
		pivotType:      nearestPivotType,
	}

	switch nearestPivotType {
	case "top":
		ctx.pivotTimestamp, err = strconv.ParseInt(nearestTopTime, 10, 64)
		if err != nil {
			return nil, nil
		}
		ctx.pivotPrice, err = strconv.ParseFloat(nearestTopPrice, 64)
	case "bottom":
		ctx.pivotTimestamp, err = strconv.ParseInt(nearestBottomTime, 10, 64)
		if err != nil {
			return nil, nil
		}
		ctx.pivotPrice, err = strconv.ParseFloat(nearestBottomPrice, 64)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, nil
	}

	if ctx.pivotTimestamp <= ctx.clickTimestamp {
		return nil, fmt.Errorf(noFuturePivotError)
	}

	return ctx, nil
}

// HandleGetAIAnalysis handles GET /api/news/analysis - get AI analysis for a specific 4-hour block
func (h *NewsHandler) HandleGetAIAnalysis(c *gin.Context) {
	timestampStr := c.Query("timestamp")
	if timestampStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp parameter is required"})
		return
	}

	// Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timestamp format"})
		return
	}

	// Check if force refresh is requested
	forceRefresh := c.Query("force_refresh") == "true"

	// Get language
	language := c.DefaultQuery("language", "en")
	rawSymbol := strings.TrimSpace(strings.ToUpper(c.Query("symbol")))
	symbol := rawSymbol
	if symbol == "" {
		symbol = defaultNewsAnalysisSymbol
		logger.Warnf("⚠️ News analysis request missing symbol, defaulting to %s", symbol)
	}

	// Parse top/bottom info from query params
	nearestTopPrice := c.Query("nearest_top_price")
	nearestTopTime := c.Query("nearest_top_time")
	nearestBottomPrice := c.Query("nearest_bottom_price")
	nearestBottomTime := c.Query("nearest_bottom_time")
	priceAtClick := c.Query("price_at_click")
	timestampAtClick := c.Query("timestamp_at_click")
	nearestPivotType := c.Query("nearest_pivot_type") // "top" or "bottom"

	if timestampAtClick != "" {
		if clickTimestamp, err := strconv.ParseInt(timestampAtClick, 10, 64); err == nil {
			if err := h.newsService.RecordManualAnalysisAnchor(clickTimestamp); err != nil {
				logger.Warnf("⚠️ Failed to record manual news-analysis anchor: %v", err)
			}
		}
	}

	rangeCtx, err := parseRangeAnalysisContext(
		timestampAtClick,
		priceAtClick,
		nearestTopPrice,
		nearestTopTime,
		nearestBottomPrice,
		nearestBottomTime,
		nearestPivotType,
	)
	if err != nil {
		if err.Error() == noFuturePivotError {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": err.Error(),
			})
			return
		}
	}

	logger.Infof(
		"📰 News analysis request context: timestamp=%d language=%s raw_symbol=%q effective_symbol=%s range_mode=%t force_refresh=%t",
		timestamp,
		language,
		rawSymbol,
		symbol,
		rangeCtx != nil,
		forceRefresh,
	)

	if rangeCtx != nil {
		analysis, err := h.newsService.GenerateRangeMarketAnalysis(
			timestamp,
			rangeCtx.clickTimestamp,
			rangeCtx.clickPrice,
			rangeCtx.pivotTimestamp,
			rangeCtx.pivotPrice,
			rangeCtx.pivotType,
			symbol,
			c.GetString("user_id"),
			language,
			forceRefresh,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate AI analysis: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, AIAnalysisResponse{
			Timestamp: analysis.Timestamp,
			Title:     analysis.Title,
			English:   analysis.English,
			Chinese:   analysis.Chinese,
			Date:      analysis.Date,
			Stars:     analysis.Stars,
		})
		return
	}

	// Convert timestamp to the start of the matching 4-hour UTC block
	bucketTimestamp := store.NormalizeAIAnalysisTimestamp(timestamp)
	targetTime := time.Unix(bucketTimestamp, 0).UTC()
	dateStr := targetTime.Format("January 2, 2006 15:04 UTC")

	technicalContext := ""
	analysisTitle := "Market Analysis"
	if symbol != "" {
		analysisTitle += " (" + symbol + ")"
	}

	userID := c.GetString("user_id")
	candidateModels, err := news.CollectAvailableAIModels(h.store, userID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to load AI models: " + err.Error()})
		return
	}

	// Try to get from database first (unless force refresh)
	// Note: Top/bottom info will be used when generating a NEW analysis, but won't force regeneration of existing ones
	if !forceRefresh {
		existingAnalysis, err := h.store.News().GetAIAnalysis(timestamp, language)
		if err == nil && existingAnalysis != nil {
			if strings.TrimSpace(existingAnalysis.Title) == analysisTitle {
				// Found in database, return it
				c.JSON(http.StatusOK, AIAnalysisResponse{
					Timestamp: existingAnalysis.Timestamp,
					Title:     existingAnalysis.Title,
					English:   existingAnalysis.English,
					Chinese:   existingAnalysis.Chinese,
					Date:      existingAnalysis.Date,
					Stars:     existingAnalysis.Stars,
				})
				return
			}
		}
	}

	// Not in database or force refresh, generate new analysis
	systemPrompt := "You are a specialized crypto market analyst. Search for relevant news and use technical context to explain the observed market move. Return bilingual markdown."
	userPrompt := buildClickTimeAnalysisPrompt(dateStr, symbol, technicalContext)

	// Call AI
	response, _, err := news.CallWithAvailableAIModels(candidateModels, 8192, 60*time.Second, systemPrompt, userPrompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate AI analysis: " + err.Error()})
		return
	}

	// Parse the JSON response
	english, chinese := parseAIResponse(response)

	analysisTimestamp := bucketTimestamp
	if timestampAtClick != "" {
		if ts, err := strconv.ParseInt(timestampAtClick, 10, 64); err == nil {
			analysisTimestamp = store.NormalizeAIAnalysisTimestamp(ts)
		}
	}

	// Save to database
	analysis := &store.AIAnalysis{
		Timestamp: analysisTimestamp,
		Date:      dateStr,
		Title:     analysisTitle,
		English:   english,
		Chinese:   chinese,
		Language:  language,
		Stars:     0, // Default to 0 stars for new analysis
	}
	if err := h.store.News().SaveAIAnalysis(analysis); err != nil {
		// Log error but don't fail the request - analysis was generated successfully
		logger.Errorf("Failed to save AI analysis to database: %v", err)
	}

	c.JSON(http.StatusOK, AIAnalysisResponse{
		Timestamp: analysis.Timestamp,
		Title:     analysis.Title,
		English:   english,
		Chinese:   chinese,
		Date:      dateStr,
		Stars:     analysis.Stars,
	})
}

func buildClickTimeAnalysisPrompt(_ string, symbol string, technicalContext string) string {
	intro := []string{
		"Analyze this price movement using technical context and your knowledge of market events.",
	}
	if symbol != "" {
		intro = append(intro, "Focus specifically on the "+symbol+" market and keep the explanation tied to that symbol.")
	}

	return news.BuildMarketAnalysisPrompt(
		"Market Analysis",
		intro,
		technicalContext,
	)
}

// HandleDeleteAIAnalysis handles DELETE /api/news/analysis/:timestamp - delete an AI analysis
func (h *NewsHandler) HandleDeleteAIAnalysis(c *gin.Context) {
	timestampStr := c.Param("timestamp")
	if timestampStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp parameter is required"})
		return
	}

	// Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timestamp format"})
		return
	}

	// Delete from database
	if err := h.store.News().DeleteAIAnalysis(timestamp); err != nil {
		if err.Error() == "record not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Analysis not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete analysis"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Analysis deleted successfully"})
}

// HandleUpdateAIAnalysisStars handles PUT /api/news/analysis/:timestamp/stars - update star rating
func (h *NewsHandler) HandleUpdateAIAnalysisStars(c *gin.Context) {
	timestampStr := c.Param("timestamp")
	if timestampStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp parameter is required"})
		return
	}

	// Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timestamp format"})
		return
	}

	// Parse request body
	var req struct {
		Stars int `json:"stars"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate stars range
	if req.Stars < 0 || req.Stars > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Stars must be between 0 and 5"})
		return
	}

	// Update in database
	if err := h.store.News().UpdateAIAnalysisStars(timestamp, req.Stars); err != nil {
		if err.Error() == "record not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Analysis not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update star rating"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Star rating updated successfully", "stars": req.Stars})
}

// HandleGetAllAIAnalyses handles GET /api/news/analyses - get all saved AI analyses
func (h *NewsHandler) HandleGetAllAIAnalyses(c *gin.Context) {
	language := c.DefaultQuery("language", "en")

	analyses, err := h.store.News().GetAllAIAnalyses(language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get AI analyses: " + err.Error()})
		return
	}

	// Convert to response format
	response := make([]AIAnalysisResponse, len(analyses))
	for i, a := range analyses {
		response[i] = AIAnalysisResponse{
			Timestamp: a.Timestamp,
			Title:     a.Title,
			English:   a.English,
			Chinese:   a.Chinese,
			Date:      a.Date,
			Stars:     a.Stars,
		}
	}

	c.JSON(http.StatusOK, response)
}

// parseAIResponse extracts english and chinese content from AI response (markdown format)
func parseAIResponse(response string) (string, string) {
	// Try to find markdown sections: ### English and ### 中文
	englishStart := -1
	englishEnd := -1
	chineseStart := -1
	chineseEnd := -1

	lines := strings.Split(response, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Detect section headers
		if strings.HasPrefix(lower, "###") {
			if strings.Contains(lower, "english") {
				englishStart = i + 1
				// Check if there's a previous section ending
				if chineseStart > 0 && chineseEnd == -1 {
					chineseEnd = i
				}
			} else if strings.Contains(lower, "中文") || strings.Contains(lower, "chinese") {
				chineseStart = i + 1
				// Check if there's a previous section ending
				if englishStart > 0 && englishEnd == -1 {
					englishEnd = i
				}
			}
		}
	}

	// If no sections found, return whole response as english
	if englishStart == -1 {
		return response, response
	}

	// Set end to end of lines if not found
	if englishEnd == -1 {
		englishEnd = len(lines)
	}
	if chineseEnd == -1 {
		chineseEnd = len(lines)
	}

	// Extract content
	var english strings.Builder
	var chinese strings.Builder

	for i := englishStart; i < englishEnd && i < len(lines); i++ {
		english.WriteString(lines[i])
		english.WriteString("\n")
	}

	for i := chineseStart; i < chineseEnd && i < len(lines); i++ {
		chinese.WriteString(lines[i])
		chinese.WriteString("\n")
	}

	englishStr := strings.TrimSpace(english.String())
	chineseStr := strings.TrimSpace(chinese.String())

	// If extraction failed, fall back to whole response
	if englishStr == "" {
		englishStr = response
	}
	if chineseStr == "" {
		chineseStr = englishStr // Use english if chinese not found
	}

	return englishStr, chineseStr
}

// extractJSONValue extracts a value from JSON string (simple parser)
func extractJSONValue(json, key string) string {
	keyPattern := `"` + key + `":`
	start := -1
	end := -1

	for i := 0; i < len(json)-len(keyPattern); i++ {
		if json[i:i+len(keyPattern)] == keyPattern {
			// Found the key, now find the value
			i += len(keyPattern)
			// Skip whitespace
			for i < len(json) && (json[i] == ' ' || json[i] == '\n' || json[i] == '\t' || json[i] == '\r') {
				i++
			}
			if i >= len(json) {
				return ""
			}
			start = i
			quoteChar := json[i]
			if quoteChar != '"' {
				return ""
			}
			i++
			for i < len(json) {
				if json[i] == '\\' && i+1 < len(json) {
					i += 2
					continue
				}
				if json[i] == quoteChar {
					end = i
					break
				}
				i++
			}
			break
		}
	}

	if start == -1 || end == -1 {
		return ""
	}

	return json[start:end]
}
