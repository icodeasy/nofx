package api

import (
	"net/http"
	"nofx/news"
	"strconv"

	"github.com/gin-gonic/gin"
)

// NewsHandler handles news-related API requests
type NewsHandler struct {
	newsService *news.Service
}

// NewNewsHandler creates a new news handler
func NewNewsHandler(newsService *news.Service) *NewsHandler {
	return &NewsHandler{
		newsService: newsService,
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
	Total int64             `json:"total"`
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

// HandleRefreshNews handles POST /api/news/refresh - trigger manual news refresh
func (h *NewsHandler) HandleRefreshNews(c *gin.Context) {
	if err := h.newsService.FetchNewsManual(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to refresh news",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "News refreshed successfully",
	})
}

// HandleNewsFeedback handles POST /api/news/:id/feedback - submit feedback for a news item
func (h *NewsHandler) HandleNewsFeedback(c *gin.Context) {
	newsID := c.Param("id")

	var req struct {
		Type string `json:"type" binding:"required,oneof=good bad"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid feedback type. Must be 'good' or 'bad'",
		})
		return
	}

	if err := h.newsService.SubmitFeedback(newsID, req.Type); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to submit feedback",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Feedback submitted successfully",
	})
}
