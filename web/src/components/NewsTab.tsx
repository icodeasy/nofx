import { useState, useEffect, useRef, useCallback } from 'react'
import { Newspaper, ExternalLink, Clock, Globe, RefreshCw } from 'lucide-react'
import { motion } from 'framer-motion'

const API_BASE = import.meta.env.VITE_API_BASE || ''

interface NewsItem {
  id: string
  title: string
  source: string
  url: string
  published_at: string
  snippet: string
}

interface NewsResponse {
  news: NewsItem[]
  total: number
}

export function NewsTab() {
  const [newsItems, setNewsItems] = useState<NewsItem[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [hasMore, setHasMore] = useState(true)
  const [offset, setOffset] = useState(0)
  const limit = 20
  const observerTarget = useRef<HTMLDivElement>(null)

  const fetchNews = useCallback(async (currentOffset: number, isLoadMore = false) => {
    try {
      if (isLoadMore) {
        setIsLoadingMore(true)
      } else {
        setIsLoading(true)
      }

      const response = await fetch(
        `${API_BASE}/news?limit=${limit}&offset=${currentOffset}`
      )

      if (!response.ok) {
        throw new Error('Failed to fetch news')
      }

      const data: NewsResponse = await response.json()

      if (isLoadMore) {
        setNewsItems((prev) => [...prev, ...data.news])
      } else {
        setNewsItems(data.news)
      }

      setHasMore(data.news.length === limit)
      setOffset(currentOffset + data.news.length)
    } catch (error) {
      console.error('Error fetching news:', error)
    } finally {
      setIsLoading(false)
      setIsLoadingMore(false)
    }
  }, [limit])

  useEffect(() => {
    fetchNews(0)
  }, [fetchNews])

  // Infinite scroll using Intersection Observer
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore && !isLoading && !isLoadingMore) {
          fetchNews(offset, true)
        }
      },
      { threshold: 0.1 }
    )

    const currentTarget = observerTarget.current
    if (currentTarget) {
      observer.observe(currentTarget)
    }

    return () => {
      if (currentTarget) {
        observer.unobserve(currentTarget)
      }
    }
  }, [hasMore, isLoading, isLoadingMore, offset, fetchNews])

  const formatDate = (dateString: string) => {
    const date = new Date(dateString)
    const now = new Date()
    const diffInMs = now.getTime() - date.getTime()
    const diffInHours = Math.floor(diffInMs / (1000 * 60 * 60))
    const diffInDays = Math.floor(diffInHours / 24)

    if (diffInHours < 1) {
      return 'Just now'
    } else if (diffInHours < 24) {
      return `${diffInHours}h ago`
    } else if (diffInDays === 1) {
      return 'Yesterday'
    } else if (diffInDays < 7) {
      return `${diffInDays}d ago`
    } else {
      return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
      })
    }
  }

  const handleRefresh = async () => {
    try {
      setIsRefreshing(true)
      const response = await fetch(`${API_BASE}/news/refresh`, {
        method: 'POST',
      })

      if (!response.ok) {
        throw new Error('Failed to refresh news')
      }

      // After refresh, reload the news from the beginning
      setOffset(0)
      setHasMore(true)
      await fetchNews(0)
    } catch (error) {
      console.error('Error refreshing news:', error)
    } finally {
      setIsRefreshing(false)
    }
  }

  return (
    <div className="h-full w-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-white/5 bg-[#0B0E11]/50">
        <div className="flex items-center gap-2">
          <Newspaper className="w-4 h-4 text-nofx-gold" />
          <h2 className="text-sm font-semibold text-white">Blockchain News</h2>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={handleRefresh}
            disabled={isRefreshing || isLoading}
            className="flex items-center gap-1.5 px-2.5 py-1 bg-nofx-gold/10 border border-nofx-gold/20 rounded text-[10px] font-medium text-nofx-gold hover:bg-nofx-gold/20 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
            title="Refresh news"
          >
            <RefreshCw className={`w-3 h-3 ${isRefreshing ? 'animate-spin' : ''}`} />
            <span>{isRefreshing ? 'Refreshing...' : 'Refresh'}</span>
          </button>
          <div className="flex items-center gap-1 text-[10px] text-nofx-text-muted">
            <Globe className="w-3 h-3" />
            <span>Google News</span>
          </div>
        </div>
      </div>

      {/* News List */}
      <div className="flex-1 overflow-y-auto custom-scrollbar">
        {isLoading && newsItems.length === 0 ? (
          <div className="flex items-center justify-center h-64">
            <div className="flex flex-col items-center gap-3">
              <div className="w-8 h-8 border-2 border-nofx-gold/30 border-t-nofx-gold rounded-full animate-spin" />
              <p className="text-xs text-nofx-text-muted">Loading news...</p>
            </div>
          </div>
        ) : newsItems.length === 0 ? (
          <div className="flex items-center justify-center h-64">
            <div className="flex flex-col items-center gap-3 text-nofx-text-muted">
              <Newspaper className="w-12 h-12 opacity-30" />
              <p className="text-sm">No news available yet</p>
              <p className="text-xs opacity-60">News will be fetched daily at 1 AM UTC</p>
            </div>
          </div>
        ) : (
          <div className="divide-y divide-white/5">
            {newsItems.map((item, index) => (
              <motion.a
                key={item.id}
                href={item.url}
                target="_blank"
                rel="noopener noreferrer"
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.02 }}
                className="block p-4 hover:bg-white/5 transition-colors group"
              >
                <div className="flex gap-3">
                  <div className="flex-1 min-w-0">
                    <h3 className="text-sm font-medium text-white group-hover:text-nofx-gold transition-colors line-clamp-2 mb-1">
                      {item.title}
                    </h3>
                    {item.snippet && (
                      <p className="text-xs text-nofx-text-muted line-clamp-2 mb-2">
                        {item.snippet}
                      </p>
                    )}
                    <div className="flex items-center gap-3 text-[10px] text-nofx-text-muted/70">
                      <span className="font-medium text-nofx-gold/80">{item.source}</span>
                      <span className="flex items-center gap-1">
                        <Clock className="w-3 h-3" />
                        {formatDate(item.published_at)}
                      </span>
                    </div>
                  </div>
                  <ExternalLink className="w-4 h-4 text-nofx-text-muted/30 group-hover:text-nofx-gold transition-colors flex-shrink-0 mt-1" />
                </div>
              </motion.a>
            ))}
          </div>
        )}

        {/* Loading indicator for infinite scroll */}
        {isLoadingMore && (
          <div className="flex items-center justify-center p-4">
            <div className="w-6 h-6 border-2 border-nofx-gold/30 border-t-nofx-gold rounded-full animate-spin" />
          </div>
        )}

        {/* Observer target */}
        {hasMore && !isLoading && <div ref={observerTarget} className="h-1" />}

        {/* End of list indicator */}
        {!hasMore && newsItems.length > 0 && (
          <div className="py-6 text-center">
            <p className="text-xs text-nofx-text-muted/50">No more news</p>
          </div>
        )}
      </div>
    </div>
  )
}
