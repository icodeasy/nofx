package news

import "strings"

// BuildMarketAnalysisPrompt returns the shared bilingual market-analysis prompt body
// with caller-specific title/intro text and optional technical context.
func BuildMarketAnalysisPrompt(title string, intro []string, technicalContext string) string {
	var sb strings.Builder

	sb.WriteString("# ")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	for _, line := range intro {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	if technicalContext != "" {
		sb.WriteString(technicalContext)
		sb.WriteString("\n")
	}

	sb.WriteString("Consider: fundamentals (news, regulations, institutions), technicals (S/R, structure, volume), market context (sentiment, correlation, liquidity), and timing.\n\n")
	sb.WriteString("**CRITICAL**: Include markdown links [Title](URL) to credible sources (CoinDesk, Bloomberg, Reuters, Cointelegraph) for all news references.\n\n")
	sb.WriteString("Output format (bilingual):\n\n")
	sb.WriteString("### English\n")
	sb.WriteString("**Primary Driver(s)**: [analysis]\n\n")
	sb.WriteString("**Key Events**: [events with links]\n\n")
	sb.WriteString("**Related News**: (if applicable)\n- [Title](URL)\n\n")
	sb.WriteString("**Sustainability**: [analysis]\n\n")
	sb.WriteString("**Trader Takeaway**: [key insight]\n\n")
	sb.WriteString("### 中文\n")
	sb.WriteString("**主要驱动因素**: [分析]\n\n")
	sb.WriteString("**关键事件**: [事件及链接]\n\n")
	sb.WriteString("**相关新闻**: (如适用)\n- [标题](URL)\n\n")
	sb.WriteString("**可持续性**: [分析]\n\n")
	sb.WriteString("**交易要点**: [关键见解]\n")

	return sb.String()
}
