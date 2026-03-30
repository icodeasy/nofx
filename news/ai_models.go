package news

import (
	"fmt"
	"nofx/logger"
	"nofx/mcp"
	"nofx/store"
	"strings"
	"time"
)

// CollectAvailableAIModels returns enabled models in preferred order:
// user models first, then default/system models, then any remaining enabled models.
func CollectAvailableAIModels(st *store.Store, userID string) ([]*store.AIModel, error) {
	seen := map[string]bool{}
	models := make([]*store.AIModel, 0)

	appendUnique := func(items []*store.AIModel) {
		for _, model := range items {
			if model == nil || seen[model.ID] {
				continue
			}
			seen[model.ID] = true
			models = append(models, model)
		}
	}

	if userID != "" {
		userModels, err := st.AIModel().ListEnabled(userID)
		if err != nil {
			return nil, err
		}
		appendUnique(userModels)
	}

	if userID != "default" {
		defaultModels, err := st.AIModel().ListEnabled("default")
		if err != nil {
			return nil, err
		}
		appendUnique(defaultModels)
	}

	anyEnabled, err := st.AIModel().ListAnyEnabled()
	if err != nil {
		return nil, err
	}
	appendUnique(anyEnabled)

	return models, nil
}

// CallWithAvailableAIModels tries enabled models in order and returns on first success.
func CallWithAvailableAIModels(
	models []*store.AIModel,
	maxTokens int,
	timeout time.Duration,
	systemPrompt string,
	userPrompt string,
) (string, *store.AIModel, error) {
	failures := make([]string, 0, len(models))

	for _, model := range models {
		if model == nil || !model.Enabled {
			continue
		}
		if strings.TrimSpace(string(model.APIKey)) == "" {
			failures = append(failures, fmt.Sprintf("%s(%s): missing API key", model.Name, model.Provider))
			continue
		}

		logger.Infof("📰 News analysis trying AI model: %s", describeAIModel(model))
		client := newNewsAIClient(model.Provider, maxTokens)
		client.SetAPIKey(string(model.APIKey), model.CustomAPIURL, model.CustomModelName)
		client.SetTimeout(timeout)

		response, err := client.CallWithMessages(systemPrompt, userPrompt)
		if err == nil {
			logger.Infof("📰 News analysis succeeded with AI model: %s", describeAIModel(model))
			return response, model, nil
		}

		logger.Warnf("⚠️ News analysis failed with AI model %s: %v", describeAIModel(model), err)
		failures = append(failures, fmt.Sprintf("%s(%s): %v", model.Name, model.Provider, err))
	}

	if len(failures) == 0 {
		return "", nil, fmt.Errorf("no enabled AI models are configured")
	}

	return "", nil, fmt.Errorf("all enabled AI models failed: %s", strings.Join(failures, " | "))
}

func newNewsAIClient(provider string, maxTokens int) mcp.AIClient {
	switch provider {
	case "qwen":
		return mcp.NewQwenClientWithOptions(mcp.WithMaxTokens(maxTokens))
	case "deepseek":
		return mcp.NewDeepSeekClientWithOptions(mcp.WithMaxTokens(maxTokens))
	case "claude":
		return mcp.NewClaudeClientWithOptions(mcp.WithMaxTokens(maxTokens))
	case "kimi":
		return mcp.NewKimiClientWithOptions(mcp.WithMaxTokens(maxTokens))
	case "gemini":
		return mcp.NewGeminiClientWithOptions(mcp.WithMaxTokens(maxTokens))
	case "grok":
		return mcp.NewGrokClientWithOptions(mcp.WithMaxTokens(maxTokens))
	case "openai":
		return mcp.NewOpenAIClientWithOptions(mcp.WithMaxTokens(maxTokens))
	default:
		return mcp.NewClient(mcp.WithMaxTokens(maxTokens))
	}
}

func describeAIModel(model *store.AIModel) string {
	if model == nil {
		return "unknown"
	}

	customModel := strings.TrimSpace(model.CustomModelName)
	if customModel == "" {
		customModel = "provider-default"
	}

	return fmt.Sprintf("%s [id=%s provider=%s model=%s user=%s]", model.Name, model.ID, model.Provider, customModel, model.UserID)
}
