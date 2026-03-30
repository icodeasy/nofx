import { useState, useEffect, useCallback, useRef } from 'react'
import { ChevronDown, ChevronUp, Brain, RefreshCw, X, TrendingUp as ChartIcon, Star } from 'lucide-react'
import { motion, AnimatePresence } from 'framer-motion'
import ReactMarkdown from 'react-markdown'
import { useLanguage } from '../contexts/LanguageContext'
import { notify } from '../lib/notify'
import { NewsChart, TopBottomInfo } from './NewsChart'

const API_BASE = import.meta.env.VITE_API_BASE || ''

interface AIAnalysis {
  timestamp: number
  title?: string
  english: string
  chinese: string
  date: string
  stars: number
  pending?: boolean
}

const normalizeAnalysisTimestamp = (timestamp: number) => {
  const bucketSeconds = 4 * 60 * 60
  return Math.floor(timestamp / bucketSeconds) * bucketSeconds
}

const sortAnalyses = (analyses: AIAnalysis[]) =>
  [...analyses].sort((a, b) => a.timestamp - b.timestamp)

const getLocale = (language: string) => (language === 'zh' ? 'zh-CN' : 'en-US')

const formatAnalysisDate = (timestamp: number, language: string) =>
  new Date(timestamp * 1000).toLocaleString(getLocale(language), {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })

const localizeAnalysisTitle = (title: string | undefined, language: string) => {
  if (!title) return title

  const locale = getLocale(language)
  return title.replace(/\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC/g, (utcText) => {
    const isoLike = utcText.replace(' UTC', ':00Z').replace(' ', 'T')
    const date = new Date(isoLike)

    if (Number.isNaN(date.getTime())) {
      return utcText
    }

    return new Intl.DateTimeFormat(locale, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }).format(date)
  })
}

const normalizeAnalysisForDisplay = (analysis: AIAnalysis, language: string): AIAnalysis => ({
  ...analysis,
  date: formatAnalysisDate(analysis.timestamp, language),
  title: localizeAnalysisTitle(analysis.title, language),
})

const hasFormedNextPivot = (info?: TopBottomInfo | null) => {
  if (!info || !info.nearestPivotType) {
    return false
  }

  if (info.nearestPivotType === 'top') {
    return !!info.nearestTop && info.nearestTop.time > info.timestampAtClick
  }

  return !!info.nearestBottom && info.nearestBottom.time > info.timestampAtClick
}

export function NewsTab() {
  const { language } = useLanguage()
  const analysisSymbol = 'BTCUSDT'

  // AI Analysis state
  const [aiPanelExpanded, setAiPanelExpanded] = useState(true)
  const [aiAnalysis, setAiAnalysis] = useState<AIAnalysis | null>(null)
  const [savedAnalyses, setSavedAnalyses] = useState<AIAnalysis[]>([])
  const [isLoadingAI, setIsLoadingAI] = useState(false)
  const [loadedFromDB, setLoadedFromDB] = useState(false)

  // Chart click state
  const [clickedTimestamp, setClickedTimestamp] = useState<number | undefined>(undefined)
  const [latestCandleTimestamp, setLatestCandleTimestamp] = useState<number | undefined>(undefined)
  const [topBottomInfo, setTopBottomInfo] = useState<TopBottomInfo | null>(null)
  const tabRefs = useRef<Record<number, HTMLDivElement | null>>({})

  const fetchAIAnalysis = useCallback(async (timestamp: number, forceRefresh = false, topBottomInfo?: TopBottomInfo | null) => {
    const analysisTimestamp = normalizeAnalysisTimestamp(timestamp)

    if (topBottomInfo && !hasFormedNextPivot(topBottomInfo)) {
      setSavedAnalyses(prev => prev.filter(a => a.timestamp !== analysisTimestamp || !a.pending))
      setIsLoadingAI(false)
      notify.info(
        language === 'zh'
          ? '该位置之后尚未形成新的顶或底，暂不生成 AI 分析'
          : 'No new top or bottom has formed after this point, so AI analysis was skipped'
      )
      return
    }

    // Check if already in local cache first (avoid API call if found)
    const existingAnalysis = savedAnalyses.find(a => a.timestamp === analysisTimestamp && !a.pending)
    if (!forceRefresh && existingAnalysis) {
      setAiAnalysis(existingAnalysis)
      setAiPanelExpanded(true)
      return
    }

    const pendingAnalysis: AIAnalysis = {
      timestamp: analysisTimestamp,
      title: '',
      english: '',
      chinese: '',
      date: formatAnalysisDate(analysisTimestamp, language),
      stars: 0,
      pending: true,
    }

    // Not in local cache - call API which will check DB first and return cached if available
    try {
      setIsLoadingAI(true)
      setAiPanelExpanded(true) // Auto-expand when loading starts
      setSavedAnalyses(prev => {
        const filtered = prev.filter(a => a.timestamp !== analysisTimestamp)
        return sortAnalyses([...filtered, pendingAnalysis])
      })
      setAiAnalysis(pendingAnalysis)

      const params = new URLSearchParams({
        timestamp: String(timestamp),
        language: language === 'zh' ? 'zh' : 'en',
        symbol: analysisSymbol,
      })
      if (forceRefresh) {
        params.set('force_refresh', 'true')
      }

      // Add top/bottom info to the request if available
      if (topBottomInfo) {
        params.set('nearest_top_price', String(topBottomInfo.nearestTop?.price || ''))
        params.set('nearest_top_time', String(topBottomInfo.nearestTop?.time || ''))
        params.set('nearest_bottom_price', String(topBottomInfo.nearestBottom?.price || ''))
        params.set('nearest_bottom_time', String(topBottomInfo.nearestBottom?.time || ''))
        params.set('price_at_click', String(topBottomInfo.priceAtClick))
        params.set('timestamp_at_click', String(topBottomInfo.timestampAtClick))
        params.set('nearest_pivot_type', topBottomInfo.nearestPivotType || '')
      }

      const url = `${API_BASE}/news/analysis?${params.toString()}`
      console.log('📊 Fetching AI analysis:', { topBottomInfo, url })

      const response = await fetch(url)

      if (!response.ok) {
        let errorMessage = 'Failed to fetch AI analysis'
        if (response.status === 422) {
          const data = await response.json().catch(() => null)
          errorMessage = data?.error || errorMessage
        }
        throw new Error(errorMessage)
      }

      const data: AIAnalysis = await response.json()
      const normalizedData = normalizeAnalysisForDisplay({ ...data, pending: false }, language)

      // Add to saved analyses list (avoid duplicates) - this updates local cache
      setSavedAnalyses(prev => {
        const filtered = prev.filter(a => a.timestamp !== normalizedData.timestamp)
        return sortAnalyses([...filtered, normalizedData])
      })

      setAiAnalysis(normalizedData)
    } catch (error) {
      setSavedAnalyses(prev => prev.filter(a => a.timestamp !== analysisTimestamp))
      if (aiAnalysis?.timestamp === analysisTimestamp) {
        setAiAnalysis(null)
      }
      console.error('Error fetching AI analysis:', error)
      const message = error instanceof Error ? error.message : ''
      if (message.includes('No next top or bottom has formed')) {
        notify.info(
          language === 'zh'
            ? '该位置之后尚未形成新的顶或底，暂不生成 AI 分析'
            : 'No new top or bottom has formed after this point, so AI analysis was skipped'
        )
      } else {
        notify.error(language === 'zh' ? '获取AI分析失败' : 'Failed to fetch AI analysis')
      }
    } finally {
      setIsLoadingAI(false)
    }
  }, [language, savedAnalyses, aiAnalysis])

  // Delete AI analysis
  const handleDeleteAnalysis = useCallback(async (timestamp: number, event: React.MouseEvent) => {
    event.stopPropagation() // Prevent tab selection

    try {
      const response = await fetch(`${API_BASE}/news/analysis/${timestamp}`, {
        method: 'DELETE',
      })

      if (!response.ok) {
        throw new Error('Failed to delete analysis')
      }

      // Calculate remaining analyses before state update
      const remaining = savedAnalyses.filter(a => a.timestamp !== timestamp)

      // Remove from local state
      setSavedAnalyses(remaining)

      // If the deleted analysis was currently selected
      if (aiAnalysis?.timestamp === timestamp) {
        if (remaining.length > 0) {
          // Switch to the first remaining analysis and show its marker
          setAiAnalysis(remaining[0])
          setClickedTimestamp(remaining[0].timestamp)
        } else {
          // No remaining analyses - clear everything
          setAiAnalysis(null)
          setClickedTimestamp(undefined)
          setTopBottomInfo(null)
        }
      }

      notify.success(language === 'zh' ? '分析已删除' : 'Analysis deleted')
    } catch (error) {
      console.error('Error deleting analysis:', error)
      notify.error(language === 'zh' ? '删除失败' : 'Failed to delete analysis')
    }
  }, [language, savedAnalyses, aiAnalysis])

  // Update star rating for an AI analysis
  const handleUpdateStars = useCallback(async (timestamp: number, stars: number) => {
    try {
      const response = await fetch(`${API_BASE}/news/analysis/${timestamp}/stars`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ stars }),
      })

      if (!response.ok) {
        throw new Error('Failed to update star rating')
      }

      // Update local state
      setSavedAnalyses(prev =>
        prev.map(a => (a.timestamp === timestamp ? { ...a, stars } : a))
      )

      if (aiAnalysis?.timestamp === timestamp) {
        setAiAnalysis({ ...aiAnalysis, stars })
      }

      notify.success(language === 'zh' ? '评分已更新' : 'Rating updated')
    } catch (error) {
      console.error('Error updating star rating:', error)
      notify.error(language === 'zh' ? '评分更新失败' : 'Failed to update rating')
    }
  }, [language, aiAnalysis])

  // Fetch all saved AI analyses from database on mount
  const fetchAllAnalyses = useCallback(async () => {
    try {
      const response = await fetch(
        `${API_BASE}/news/analyses?language=${language === 'zh' ? 'zh' : 'en'}`
      )

      if (!response.ok) {
        return // Silently fail - no saved analyses yet
      }

      const data: AIAnalysis[] = await response.json()

      if (data.length > 0) {
        const sortedData = sortAnalyses(data.map(analysis => normalizeAnalysisForDisplay(analysis, language)))
        setSavedAnalyses(sortedData)
        // Set the most recent analysis as current
        setAiAnalysis(sortedData[sortedData.length - 1])
        setLoadedFromDB(true)
      }
    } catch (error) {
      console.error('Error fetching saved analyses:', error)
    }
  }, [language])

  // Fetch all saved AI analyses from database on mount
  useEffect(() => {
    fetchAllAnalyses()
  }, [fetchAllAnalyses])

  useEffect(() => {
    if (!aiPanelExpanded || !aiAnalysis) {
      return
    }

    const activeTab = tabRefs.current[aiAnalysis.timestamp]
    activeTab?.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' })
  }, [aiAnalysis, aiPanelExpanded, savedAnalyses])

  useEffect(() => {
    if (!aiAnalysis) {
      setClickedTimestamp(undefined)
      setTopBottomInfo(null)
      return
    }

    setClickedTimestamp(aiAnalysis.timestamp)
  }, [aiAnalysis])

  const handleChartClick = async (timestamp: number, chartTopBottomInfo?: TopBottomInfo) => {
    console.log('📊 Chart clicked at timestamp:', timestamp, 'Date:', new Date(timestamp * 1000).toLocaleString(), 'TopBottomInfo:', chartTopBottomInfo)

    // Set the clicked timestamp to show a mark on the chart
    setClickedTimestamp(timestamp)

    // Update top/bottom info if provided directly from chart
    if (chartTopBottomInfo) {
      setTopBottomInfo(chartTopBottomInfo)
    }

    // Fetch AI analysis for this timestamp - don't force refresh, let it use cache/DB first
    fetchAIAnalysis(timestamp, false, chartTopBottomInfo || topBottomInfo || undefined)
  }

  // Handle top/bottom info from chart click
  const handleTopBottomChange = (info: TopBottomInfo) => {
    console.log('📊 Top/Bottom info:', info)
    setTopBottomInfo(info)
  }

  return (
    <div className="w-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-white/5 bg-[#0B0E11]/50">
        <div className="flex items-center gap-2">
          <ChartIcon className="w-4 h-4 text-nofx-gold" />
          <h2 className="text-sm font-semibold text-white">Market Analysis</h2>
        </div>
      </div>

      {/* Market Chart */}
      <div className="border-b border-white/5">
        <NewsChart
          symbol={analysisSymbol}
          interval="4h"
          height={300}
          exchange="binance"
          onChartClick={handleChartClick}
          onTopBottomChange={handleTopBottomChange}
          clickedTimestamp={clickedTimestamp}
          onLatestCandle={setLatestCandleTimestamp}
        />
      </div>

      {/* AI Analysis Panel - Collapsible */}
      <AnimatePresence>
        {(aiAnalysis || isLoadingAI) && (
          <motion.div
            initial={{ maxHeight: 0, opacity: 0 }}
            animate={{ maxHeight: aiPanelExpanded ? 5000 : 48, opacity: 1 }}
            exit={{ maxHeight: 0, opacity: 0 }}
            transition={{ duration: 0.3 }}
            className="border-b border-white/5 overflow-hidden flex flex-col"
          >
            {/* AI Panel Header - Fixed */}
            <div
              className="flex items-center justify-between px-4 py-3 bg-[#0B0E11]/80 cursor-pointer hover:bg-white/5 transition-colors"
              style={{ height: '48px', flexShrink: 0 }}
              onClick={() => setAiPanelExpanded(!aiPanelExpanded)}
            >
              <div className="flex items-center gap-2">
                <Brain className="w-4 h-4 text-nofx-gold" />
                <span className="text-xs font-medium text-white">
                  🤖 {language === 'zh' ? 'AI 市场分析' : 'AI Market Analysis'}
                </span>
                {savedAnalyses.length > 0 && (
                  <span className="text-xs text-nofx-text-muted">
                    ({savedAnalyses.length})
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                {aiAnalysis && (
                  <button
                    onClick={() => fetchAIAnalysis(aiAnalysis.timestamp, true)}
                    disabled={isLoadingAI}
                    className="text-xs text-nofx-text-muted hover:text-white transition-colors disabled:opacity-50"
                    title={language === 'zh' ? '刷新分析' : 'Refresh analysis'}
                  >
                    <RefreshCw className={`w-4 h-4 ${isLoadingAI ? 'animate-spin' : ''}`} />
                  </button>
                )}
                <button className="text-xs text-nofx-text-muted hover:text-white transition-colors">
                  {aiPanelExpanded ? (
                    <ChevronUp className="w-4 h-4" />
                  ) : (
                    <ChevronDown className="w-4 h-4" />
                  )}
                </button>
              </div>
            </div>

            {/* AI Analysis Tabs */}
            {aiPanelExpanded && savedAnalyses.length > 0 && (
              <div className="flex-shrink-0 flex gap-1 px-2 py-2 bg-[#0B0E11]/50 border-b border-white/5 overflow-x-auto custom-scrollbar">
                {savedAnalyses.map((analysis) => (
                  <div
                    key={analysis.timestamp}
                    ref={(node) => {
                      tabRefs.current[analysis.timestamp] = node
                    }}
                    className={`group flex-shrink-0 flex items-center gap-1 px-3 py-1.5 text-xs rounded-md transition-colors ${
                      aiAnalysis?.timestamp === analysis.timestamp
                        ? 'bg-nofx-gold/20 text-nofx-gold'
                        : 'text-nofx-text-muted hover:bg-white/5 hover:text-white'
                    }`}
                  >
                    <button
                      onClick={() => {
                        setAiAnalysis(analysis)
                        setClickedTimestamp(analysis.timestamp)
                      }}
                      className="flex-1 flex items-center gap-1.5"
                    >
                      <span>{formatAnalysisDate(analysis.timestamp, language)}</span>
                      {analysis.pending ? (
                        <span className="text-[10px] text-nofx-text-muted">
                          {language === 'zh' ? '生成中...' : 'Loading...'}
                        </span>
                      ) : analysis.stars > 0 ? (
                        <span className="flex items-center gap-0.5">
                          <Star className="w-3 h-3 fill-nofx-gold text-nofx-gold" />
                          <span className="text-[10px]">{analysis.stars}</span>
                        </span>
                      ) : null}
                    </button>
                    <button
                      onClick={(e) => handleDeleteAnalysis(analysis.timestamp, e)}
                      className="opacity-0 group-hover:opacity-100 hover:opacity-100 hover:text-red-400 transition-opacity"
                      title={language === 'zh' ? '删除分析' : 'Delete analysis'}
                    >
                      <X className="w-3 h-3" />
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* AI Panel Content - Unlimited */}
            {aiPanelExpanded && (
              <div className="px-4 py-3 bg-[#0B0E11]/50">
                {isLoadingAI ? (
                  <div className="flex items-center justify-center py-4">
                    <div className="w-6 h-6 border-2 border-nofx-gold/30 border-t-nofx-gold rounded-full animate-spin" />
                    <span className="ml-2 text-xs text-nofx-text-muted">
                      {language === 'zh' ? 'AI 分析中...' : 'Generating AI analysis...'}
                    </span>
                  </div>
                ) : aiAnalysis ? (
                  <div className="space-y-3">
                    <div className="flex flex-wrap items-center gap-3">
                      {/* Star Rating */}
                      <div className="flex flex-shrink-0 items-center gap-1">
                        {[1, 2, 3, 4, 5].map((star) => (
                          <button
                            key={star}
                            onClick={() => handleUpdateStars(aiAnalysis.timestamp, star)}
                            className="transition-colors hover:scale-110"
                            title={`${star} ${language === 'zh' ? '星' : 'star'}${star > 1 ? 's' : ''}`}
                          >
                            <Star
                              className={`w-4 h-4 ${
                                star <= (aiAnalysis.stars || 0)
                                  ? 'fill-nofx-gold text-nofx-gold'
                                  : 'text-gray-600'
                              }`}
                            />
                          </button>
                        ))}
                        {aiAnalysis.stars > 0 && (
                          <span className="ml-2 text-xs text-nofx-text-muted">
                            ({aiAnalysis.stars}/5)
                          </span>
                        )}
                      </div>

                      {aiAnalysis.title && (
                        <div className="min-w-0 flex-1 text-sm font-semibold text-white">
                          {localizeAnalysisTitle(aiAnalysis.title, language)}
                        </div>
                      )}
                    </div>

                    {(aiAnalysis.title || aiAnalysis.stars > 0) && (
                      <div className="border-t border-white/10" />
                    )}

                    {/* English Version */}
                    <div>
                      <h4 className="text-xs font-medium text-nofx-gold mb-1">English</h4>
                      <div className="text-xs text-nofx-text-muted leading-relaxed prose prose-invert prose-sm max-w-none">
                        <ReactMarkdown>{aiAnalysis.english}</ReactMarkdown>
                      </div>
                    </div>

                    {/* Divider */}
                    <div className="border-t border-white/10" />

                    {/* Chinese Version */}
                    <div>
                      <h4 className="text-xs font-medium text-nofx-gold mb-1">中文</h4>
                      <div className="text-xs text-nofx-text-muted leading-relaxed prose prose-invert prose-sm max-w-none">
                        <ReactMarkdown>{aiAnalysis.chinese}</ReactMarkdown>
                      </div>
                    </div>
                  </div>
                ) : null}
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>

    </div>
  )
}
