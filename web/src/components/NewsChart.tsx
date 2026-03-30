import { useEffect, useRef, useState, useCallback } from 'react'
import {
  createChart,
  IChartApi,
  ISeriesApi,
  UTCTimestamp,
  CandlestickSeries,
  HistogramSeries,
  Time,
  createSeriesMarkers,
} from 'lightweight-charts'
import { useLanguage } from '../contexts/LanguageContext'
import { httpClient } from '../lib/httpClient'
import { findTopsAndBottoms, type Kline as IndicatorKline } from '../utils/indicators'

interface Kline {
  time: number
  open: number
  high: number
  low: number
  close: number
  volume: number
  quoteVolume: number
}

// Top/bottom info for AI analysis
export interface TopBottomInfo {
  nearestTop: { time: number; price: number } | null
  nearestBottom: { time: number; price: number } | null
  currentPrice: number
  priceAtClick: number
  timestampAtClick: number   // timestamp at the clicked point
  diffToTop: number      // percentage, negative = need to drop to reach top
  diffToBottom: number   // percentage, positive = need to rise to reach bottom
  nearestPivotType: 'top' | 'bottom' | null  // which pivot is closer in absolute terms
}

interface NewsChartProps {
  symbol: string
  interval?: string
  height?: number
  exchange?: string
  onChartClick?: (timestamp: number, topBottomInfo?: TopBottomInfo) => void
  onTopBottomChange?: (info: TopBottomInfo) => void
  clickedTimestamp?: number
  onLatestCandle?: (timestamp: number) => void
}

// Format large numbers
const formatVolume = (value: number): string => {
  if (value >= 1e9) return (value / 1e9).toFixed(2) + 'B'
  if (value >= 1e6) return (value / 1e6).toFixed(2) + 'M'
  if (value >= 1e3) return (value / 1e3).toFixed(2) + 'K'
  return value.toFixed(2)
}

const toLocalDate = (time: Time): Date => {
  if (typeof time === 'number') {
    return new Date(time * 1000)
  }

  if (typeof time === 'string') {
    return new Date(time)
  }

  return new Date(time.year, time.month - 1, time.day)
}

const formatLocalChartTime = (time: Time, locale: string): string =>
  new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(toLocalDate(time))

export function NewsChart({
  symbol = 'BTCUSDT',
  interval = '1h',
  height = 300,
  exchange = 'binance',
  onChartClick,
  onTopBottomChange,
  clickedTimestamp,
  onLatestCandle,
}: NewsChartProps) {
  const { language } = useLanguage()
  const chartLocale = language === 'zh' ? 'zh-CN' : 'en-US'
  const chartContainerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const candlestickSeriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  const seriesMarkersRef = useRef<any>(null)
  const topsBottomsMarkersRef = useRef<any>(null)
  const klineDataRef = useRef<Kline[]>([])

  const [loading, setLoading] = useState(true)
  const [tooltipData, setTooltipData] = useState<any>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)

  // Function to calculate tops/bottoms for a given timestamp
  const calculateTopsBottomsForTimestamp = useCallback((timestamp: number) => {
    const klineAtClick = klineDataRef.current.find(k => k.time === timestamp)
    const priceAtClick = klineAtClick?.close || 0

    if (klineDataRef.current.length === 0 || priceAtClick === 0) {
      return null
    }

    const indicatorKlines: IndicatorKline[] = klineDataRef.current.map((k: Kline) => ({
      time: k.time as number,
      open: k.open,
      high: k.high,
      low: k.low,
      close: k.close,
    }))
    const boxRatio = 1.06
    const { tops, bottoms } = findTopsAndBottoms(indicatorKlines, boxRatio)

    // Find the next top AFTER the clicked timestamp
    let nearestTop: { time: number; price: number } | null = null
    let minTopTimeDiff = Infinity
    for (const top of tops) {
      if (top.time > timestamp) {
        const timeDiff = top.time - timestamp
        if (timeDiff < minTopTimeDiff) {
          minTopTimeDiff = timeDiff
          nearestTop = { time: top.time, price: top.price }
        }
      }
    }

    // Find the next bottom AFTER the clicked timestamp
    let nearestBottom: { time: number; price: number } | null = null
    let minBottomTimeDiff = Infinity
    for (const bottom of bottoms) {
      if (bottom.time > timestamp) {
        const timeDiff = bottom.time - timestamp
        if (timeDiff < minBottomTimeDiff) {
          minBottomTimeDiff = timeDiff
          nearestBottom = { time: bottom.time, price: bottom.price }
        }
      }
    }

    // Calculate price differences to the next pivots
    const topPriceDiff = nearestTop ? Math.abs(nearestTop.price - priceAtClick) : 0
    const bottomPriceDiff = nearestBottom ? Math.abs(nearestBottom.price - priceAtClick) : 0

    // Keep only the pivot with the bigger price difference (either top OR bottom, not both)
    let nearestPivotType: 'top' | 'bottom' | null = null
    if (nearestTop && nearestBottom) {
      if (topPriceDiff > bottomPriceDiff) {
        nearestPivotType = 'top'
        nearestBottom = null
      } else {
        nearestPivotType = 'bottom'
        nearestTop = null
      }
    } else if (nearestTop) {
      nearestPivotType = 'top'
    } else if (nearestBottom) {
      nearestPivotType = 'bottom'
    }

    // Calculate percentage differences
    const diffToTop = nearestTop ? ((nearestTop.price - priceAtClick) / priceAtClick) * 100 : 0
    const diffToBottom = nearestBottom ? ((priceAtClick - nearestBottom.price) / priceAtClick) * 100 : 0

    const currentPrice = klineDataRef.current[klineDataRef.current.length - 1]?.close || 0

    const topBottomInfo: TopBottomInfo = {
      nearestTop,
      nearestBottom,
      currentPrice,
      priceAtClick,
      timestampAtClick: timestamp,
      diffToTop,
      diffToBottom,
      nearestPivotType,
    }

    // Add markers for the found pivot points
    const pivotMarkers: any[] = []

    if (nearestTop) {
      pivotMarkers.push({
        time: nearestTop.time as UTCTimestamp,
        position: 'aboveBar' as const,
        color: '#10b981',
        shape: 'circle' as const,
        text: 'Top',
        size: 2,
      })
    }

    if (nearestBottom) {
      pivotMarkers.push({
        time: nearestBottom.time as UTCTimestamp,
        position: 'belowBar' as const,
        color: '#ef4444',
        shape: 'circle' as const,
        text: 'Bottom',
        size: 2,
      })
    }

    // Update or create pivot markers
    if (pivotMarkers.length > 0 && candlestickSeriesRef.current) {
      if (!topsBottomsMarkersRef.current) {
        topsBottomsMarkersRef.current = createSeriesMarkers(candlestickSeriesRef.current, pivotMarkers)
      } else {
        topsBottomsMarkersRef.current.setMarkers(pivotMarkers)
      }
    }

    return topBottomInfo
  }, [])

  // Market stats
  const [marketStats, setMarketStats] = useState<{
    price: number
    priceChange: number
    priceChangePercent: number
    high: number
    low: number
    volume: number
    quoteVolume: number
  } | null>(null)

  // Fetch kline data
  const fetchKlineData = async (symbol: string, interval: string) => {
    try {
      // Calculate limit based on interval for ~3 years of data
      let limit = 500
      if (interval === '4h') {
        limit = 1500
      } else if (interval === '1d') {
        limit = 1100 // ~3 years of daily candles
      } else if (interval === '1w') {
        limit = 160 // ~3 years of weekly candles
      } else if (interval === '1M') {
        limit = 36 // 3 years of monthly candles
      }

      const klineUrl = `/klines?symbol=${symbol}&interval=${interval}&limit=${limit}&exchange=${exchange}`
      const result = await httpClient.get(klineUrl)

      if (!result.success || !result.data) {
        throw new Error('Failed to fetch kline data')
      }

      const rawData = result.data.map((candle: any) => ({
        time: Math.floor(candle.openTime / 1000) as UTCTimestamp,
        open: candle.open,
        high: candle.high,
        low: candle.low,
        close: candle.close,
        volume: candle.volume,
        quoteVolume: candle.quoteVolume,
      }))

      const sortedData = rawData.sort((a: any, b: any) => a.time - b.time)
      const dedupedData = sortedData.filter((item: any, index: number, arr: any[]) =>
        index === 0 || item.time !== arr[index - 1].time
      )

      return dedupedData
    } catch (err) {
      console.error('[NewsChart] Error fetching kline:', err)
      throw err
    }
  }

  // Initialize chart
  useEffect(() => {
    if (!chartContainerRef.current) return

    const chart = createChart(chartContainerRef.current, {
      width: chartContainerRef.current.clientWidth || 800,
      height: chartContainerRef.current.clientHeight || height,
      layout: {
        background: { color: '#0B0E11' },
        textColor: '#B7BDC6',
        fontSize: 12,
      },
      grid: {
        vertLines: { color: 'rgba(43, 49, 57, 0.2)', visible: true },
        horzLines: { color: 'rgba(43, 49, 57, 0.2)', visible: true },
      },
      crosshair: {
        mode: 1,
        vertLine: {
          color: 'rgba(240, 185, 11, 0.5)',
          width: 1,
          style: 2,
          labelBackgroundColor: '#F0B90B',
        },
        horzLine: {
          color: 'rgba(240, 185, 11, 0.5)',
          width: 1,
          style: 2,
          labelBackgroundColor: '#F0B90B',
        },
      },
      rightPriceScale: {
        borderColor: '#2B3139',
        scaleMargins: { top: 0.1, bottom: 0.15 },
      },
      localization: {
        locale: chartLocale,
        timeFormatter: (time: Time) => formatLocalChartTime(time, chartLocale),
      },
      timeScale: {
        borderColor: '#2B3139',
        timeVisible: true,
        secondsVisible: false,
        rightOffset: 5,
        barSpacing: 8,
      },
      handleScroll: {
        mouseWheel: true,
        pressedMouseMove: true,
        horzTouchDrag: true,
        vertTouchDrag: true,
      },
      handleScale: {
        axisPressedMouseMove: true,
        mouseWheel: true,
        pinch: true,
      },
    })

    chartRef.current = chart

    // Candlestick series
    const candlestickSeries = chart.addSeries(CandlestickSeries, {
      upColor: '#0ECB81',
      downColor: '#F6465D',
      borderUpColor: '#0ECB81',
      borderDownColor: '#F6465D',
      wickUpColor: '#0ECB81',
      wickDownColor: '#F6465D',
    })
    candlestickSeriesRef.current = candlestickSeries as any

    // Volume series (always shown)
    const volumeSeries = chart.addSeries(HistogramSeries, {
      color: '#26a69a',
      priceFormat: { type: 'volume' },
      priceScaleId: '',
      lastValueVisible: false,
      priceLineVisible: false,
    })
    volumeSeriesRef.current = volumeSeries as any

    // Resize observer
    const resizeObserver = new ResizeObserver((entries) => {
      if (entries.length === 0 || !entries[0].contentRect) return
      const { width, height } = entries[0].contentRect
      chart.applyOptions({ width, height })
    })

    if (chartContainerRef.current) {
      resizeObserver.observe(chartContainerRef.current)
    }

    // Crosshair move handler
    chart.subscribeCrosshairMove((param) => {
      if (!param.time || !param.point || !candlestickSeriesRef.current) {
        setTooltipData(null)
        return
      }

      const data = param.seriesData.get(candlestickSeriesRef.current as any)
      if (!data) {
        setTooltipData(null)
        return
      }

      const candleData = data as any
      const kline = klineDataRef.current.find(k => k.time === param.time)

      setTooltipData({
        time: param.time,
        open: candleData.open,
        high: candleData.high,
        low: candleData.low,
        close: candleData.close,
        volume: kline?.volume || 0,
        quoteVolume: kline?.quoteVolume || 0,
        x: param.point.x,
        y: param.point.y,
      })
    })

    // Click handler - calculate nearest top/bottom when clicked
    if (onChartClick || onTopBottomChange) {
      chart.subscribeClick((param) => {
        if (param.time) {
          const timestamp = typeof param.time === 'number'
            ? param.time
            : Math.floor(new Date(param.time as string).getTime() / 1000)

          const topBottomInfo = calculateTopsBottomsForTimestamp(timestamp)

          if (topBottomInfo) {
            // Call callbacks
            if (onTopBottomChange) {
              onTopBottomChange(topBottomInfo)
            }
            if (onChartClick) {
              onChartClick(timestamp, topBottomInfo)
            }
          }
        }
      })
    }

    return () => {
      resizeObserver.disconnect()
      chart.remove()
    }
  }, [])

  useEffect(() => {
    chartRef.current?.applyOptions({
      localization: {
        locale: chartLocale,
        timeFormatter: (time: Time) => formatLocalChartTime(time, chartLocale),
      },
    })
  }, [chartLocale])

  // Load data
  useEffect(() => {
    const loadData = async () => {
      if (!candlestickSeriesRef.current) return

      setLoading(true)

      try {
        const klineData = await fetchKlineData(symbol, interval)
        candlestickSeriesRef.current.setData(klineData)
        klineDataRef.current = klineData

        // Market stats
        if (klineData.length > 1) {
          const latestKline = klineData[klineData.length - 1]
          const prevKline = klineData[klineData.length - 2]
          const priceChange = latestKline.close - prevKline.close
          const priceChangePercent = (priceChange / prevKline.close) * 100

          setMarketStats({
            price: latestKline.close,
            priceChange,
            priceChangePercent,
            high: latestKline.high,
            low: latestKline.low,
            volume: latestKline.volume || 0,
            quoteVolume: latestKline.quoteVolume || 0,
          })

          // Notify parent about latest candle timestamp
          if (onLatestCandle) {
            onLatestCandle(latestKline.time)
          }
        }

        // Volume (always shown)
        if (volumeSeriesRef.current) {
          const volumeData = klineData.map((k: Kline) => ({
            time: k.time,
            value: k.volume || 0,
            color: k.close >= k.open ? 'rgba(14, 203, 129, 0.5)' : 'rgba(246, 70, 93, 0.5)',
          }))
          volumeSeriesRef.current.setData(volumeData)
        }

        chartRef.current?.timeScale().fitContent()
        setLoading(false)
      } catch (err: any) {
        console.error('[NewsChart] Error loading data:', err)
        setLoading(false)
      }
    }

    loadData()
  }, [symbol, interval, exchange, onLatestCandle])

  // Handle clicked timestamp marker - using createSeriesMarkers
  // Also recalculate tops/bottoms when timestamp is set externally (e.g., from news click)
  useEffect(() => {
    if (!candlestickSeriesRef.current) return

    // Clear markers when clickedTimestamp is undefined
    if (!clickedTimestamp) {
      if (seriesMarkersRef.current) {
        seriesMarkersRef.current.setMarkers([])
      }
      if (topsBottomsMarkersRef.current) {
        topsBottomsMarkersRef.current.setMarkers([])
      }
      return
    }

    try {
      const clickMarker = {
        time: clickedTimestamp as Time,
        position: 'belowBar' as const,
        color: '#F0B90B',
        shape: 'arrowUp' as const,
        text: '',
        size: 2,
      }

      // Create or update markers using v5 API
      if (!seriesMarkersRef.current) {
        seriesMarkersRef.current = createSeriesMarkers(candlestickSeriesRef.current, [clickMarker])
      } else {
        seriesMarkersRef.current.setMarkers([clickMarker])
      }

      console.log('[NewsChart] ✅ Click mark added at:', clickedTimestamp, new Date(clickedTimestamp * 1000).toLocaleString())

      // Recalculate tops/bottoms for this timestamp and notify parent
      const topBottomInfo = calculateTopsBottomsForTimestamp(clickedTimestamp)
      if (topBottomInfo && onTopBottomChange) {
        onTopBottomChange(topBottomInfo)
        console.log('[NewsChart] 🔄 Recalculated tops/bottoms for timestamp:', clickedTimestamp)
      }
    } catch (err) {
      console.error('[NewsChart] ❌ Failed to add click mark:', err)
    }
  }, [clickedTimestamp, calculateTopsBottomsForTimestamp, onTopBottomChange])

  return (
    <div
      className="relative shadow-xl"
      style={{
        background: 'linear-gradient(180deg, #0F1215 0%, #0B0E11 100%)',
        borderRadius: '12px',
        overflow: 'hidden',
        border: '1px solid rgba(43, 49, 57, 0.5)',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-2"
        style={{ borderBottom: '1px solid rgba(43, 49, 57, 0.6)', background: '#0D1117', flexShrink: 0 }}
      >
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-sm font-bold text-white">{symbol}</span>
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-[#1F2937] text-gray-400">{interval}</span>
          </div>

          {marketStats && (
            <div className="flex items-center gap-3 pl-3 border-l border-[#2B3139]">
              <span
                className="text-base font-bold tabular-nums"
                style={{ color: marketStats.priceChange >= 0 ? '#10B981' : '#EF4444' }}
              >
                {marketStats.price.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
              </span>
              <span
                className="text-xs font-medium px-1.5 py-0.5 rounded tabular-nums"
                style={{
                  background: marketStats.priceChange >= 0 ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                  color: marketStats.priceChange >= 0 ? '#10B981' : '#EF4444',
                }}
              >
                {marketStats.priceChange >= 0 ? '+' : ''}{marketStats.priceChangePercent.toFixed(2)}%
              </span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-1.5">
          {loading && (
            <span className="text-[10px] text-yellow-400 animate-pulse mr-2">
              {language === 'zh' ? '更新中...' : 'Updating...'}
            </span>
          )}
        </div>
      </div>

      {/* Chart Container */}
      <div style={{ position: 'relative', flex: 1, minHeight: 0 }}>
        <div ref={chartContainerRef} style={{ height: '100%', width: '100%' }} />

        {/* Tooltip */}
        {tooltipData && (
          <div
            ref={tooltipRef}
            style={{
              position: 'absolute',
              left: '10px',
              top: '10px',
              padding: '8px 12px',
              background: 'rgba(15, 18, 21, 0.95)',
              border: '1px solid rgba(240, 185, 11, 0.3)',
              borderRadius: '6px',
              color: '#EAECEF',
              fontSize: '12px',
              fontFamily: 'monospace',
              pointerEvents: 'none',
              zIndex: 10,
            }}
          >
            <div style={{ marginBottom: '6px', color: '#F0B90B', fontWeight: 'bold', fontSize: '11px' }}>
              {formatLocalChartTime(tooltipData.time as Time, chartLocale)}
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: '11px' }}>
              <span style={{ color: '#848E9C' }}>O:</span>
              <span style={{ color: '#EAECEF' }}>{tooltipData.open?.toFixed(2)}</span>
              <span style={{ color: '#848E9C' }}>H:</span>
              <span style={{ color: '#0ECB81' }}>{tooltipData.high?.toFixed(2)}</span>
              <span style={{ color: '#848E9C' }}>L:</span>
              <span style={{ color: '#F6465D' }}>{tooltipData.low?.toFixed(2)}</span>
              <span style={{ color: '#848E9C' }}>C:</span>
              <span style={{ color: tooltipData.close >= tooltipData.open ? '#0ECB81' : '#F6465D', fontWeight: 'bold' }}>
                {tooltipData.close?.toFixed(2)}
              </span>
              {tooltipData.volume > 0 && (
                <>
                  <span style={{ color: '#848E9C' }}>Vol:</span>
                  <span style={{ color: '#3B82F6' }}>{formatVolume(tooltipData.volume)}</span>
                </>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
