package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"nofx/logger"
	"nofx/provider/coinank/coinank_api"
	"nofx/provider/coinank/coinank_enum"
	"nofx/provider/hyperliquid"
	"nofx/types"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FundingRateCache is the funding rate cache structure
// Binance Funding Rate only updates every 8 hours, using 1-hour cache can significantly reduce API calls
type FundingRateCache struct {
	Rate      float64
	UpdatedAt time.Time
}

var (
	fundingRateMap sync.Map // map[string]*FundingRateCache
	frCacheTTL     = 1 * time.Hour
)

// TimeframeFetchConfig defines configuration for fetching market data with multiple timeframes
type TimeframeFetchConfig struct {
	Timeframes       []string // List of timeframes to fetch, e.g. ["5m", "15m", "1h", "4h"]
	PrimaryTimeframe string   // Primary timeframe for calculating current indicators
	DisplayCount     int      // Number of klines to include in output for display (BOX uses all fetched data)

	// Indicator parameter configurations (full ParamConfig objects)
	EMA  *types.EMAParamConfig  // EMA configuration
	RSI  *types.RSIParamConfig  // RSI configuration
	MACD *types.MACDParamConfig // MACD configuration
	ATR  *types.ATRParamConfig  // ATR configuration
	BOLL *types.BOLLParamConfig // Bollinger Bands configuration
	BOX  *types.BOXParamConfig  // Expectation Box configuration
}

// IndicatorCalculationConfig defines configuration for calculating indicators on a single timeframe
type IndicatorCalculationConfig struct {
	DisplayCount int     // Number of data points to include in output (for display)

	// Indicator parameter configurations (full ParamConfig objects)
	EMA  *types.EMAParamConfig  // EMA configuration
	RSI  *types.RSIParamConfig  // RSI configuration
	MACD *types.MACDParamConfig // MACD configuration
	ATR  *types.ATRParamConfig  // ATR configuration
	BOLL *types.BOLLParamConfig // Bollinger Bands configuration
	BOX  *types.BOXParamConfig  // Expectation Box configuration
}

// ========== Conversion Functions (avoid circular dependency with store) ==========
// These convert store types to market types - keep in sync with store.IndicatorConfig

// NOTE: Since market package cannot import store, the caller must convert
// Example in api/strategy.go:
//   fetchConfig := market.TimeframeFetchConfig{
//     EMA: &market.EMAParamConfig{Periods: emaConfig.Periods},
//     ...
//   }

// NewTimeframeFetchConfig creates a TimeframeFetchConfig from ParamConfig objects
// This helper is provided for convenience, but direct construction is also fine
func NewTimeframeFetchConfig(
	timeframes []string,
	primaryTimeframe string,
	displayCount int,
	ema *types.EMAParamConfig,
	rsi *types.RSIParamConfig,
	atr *types.ATRParamConfig,
	boll *types.BOLLParamConfig,
	box *types.BOXParamConfig,
) TimeframeFetchConfig {
	return TimeframeFetchConfig{
		Timeframes:       timeframes,
		PrimaryTimeframe: primaryTimeframe,
		DisplayCount:     displayCount,
		EMA:              ema,
		RSI:              rsi,
		ATR:              atr,
		BOLL:             boll,
		BOX:              box,
	}
}

// Note: Kline data now uses free/open API (coinank_api.Kline) which doesn't require authentication

// getKlinesFromCoinAnk fetches kline data from CoinAnk API (replacement for WSMonitorCli)
func getKlinesFromCoinAnk(symbol, interval string, limit int) ([]Kline, error) {
	// Map interval string to coinank enum
	var coinankInterval coinank_enum.Interval
	switch interval {
	case "1m":
		coinankInterval = coinank_enum.Minute1
	case "3m":
		coinankInterval = coinank_enum.Minute3
	case "5m":
		coinankInterval = coinank_enum.Minute5
	case "15m":
		coinankInterval = coinank_enum.Minute15
	case "30m":
		coinankInterval = coinank_enum.Minute30
	case "1h":
		coinankInterval = coinank_enum.Hour1
	case "2h":
		coinankInterval = coinank_enum.Hour2
	case "4h":
		coinankInterval = coinank_enum.Hour4
	case "6h":
		coinankInterval = coinank_enum.Hour6
	case "8h":
		coinankInterval = coinank_enum.Hour8
	case "12h":
		coinankInterval = coinank_enum.Hour12
	case "1d":
		coinankInterval = coinank_enum.Day1
	case "3d":
		coinankInterval = coinank_enum.Day3
	case "1w":
		coinankInterval = coinank_enum.Week1
	default:
		return nil, fmt.Errorf("unsupported interval: %s", interval)
	}

	// Call CoinAnk free/open API (no authentication required)
	ctx := context.Background()
	ts := time.Now().UnixMilli()
	// Use "To" side to search backward from current time (get historical klines)
	coinankKlines, err := coinank_api.Kline(ctx, symbol, coinank_enum.Binance, ts, coinank_enum.To, limit, coinankInterval)
	if err != nil {
		return nil, fmt.Errorf("CoinAnk API error: %w", err)
	}

	// Convert coinank kline format to market.Kline format
	klines := make([]Kline, len(coinankKlines))
	for i, ck := range coinankKlines {
		klines[i] = Kline{
			OpenTime:  ck.StartTime,
			Open:      ck.Open,
			High:      ck.High,
			Low:       ck.Low,
			Close:     ck.Close,
			Volume:    ck.Volume,
			CloseTime: ck.EndTime,
		}
	}

	return klines, nil
}

// getKlinesFromHyperliquid fetches kline data from Hyperliquid API for xyz dex assets
func getKlinesFromHyperliquid(symbol, interval string, limit int) ([]Kline, error) {
	// Remove xyz: prefix if present for the API call
	baseCoin := strings.TrimPrefix(symbol, "xyz:")

	// Map interval to Hyperliquid format
	hlInterval := hyperliquid.MapTimeframe(interval)

	// Create Hyperliquid client
	client := hyperliquid.NewClient()

	// Fetch candles
	ctx := context.Background()
	candles, err := client.GetCandles(ctx, baseCoin, hlInterval, limit)
	if err != nil {
		return nil, fmt.Errorf("Hyperliquid API error: %w", err)
	}

	// Convert to market.Kline format
	klines := make([]Kline, len(candles))
	for i, c := range candles {
		open, _ := strconv.ParseFloat(c.Open, 64)
		high, _ := strconv.ParseFloat(c.High, 64)
		low, _ := strconv.ParseFloat(c.Low, 64)
		closePrice, _ := strconv.ParseFloat(c.Close, 64)
		volume, _ := strconv.ParseFloat(c.Volume, 64)

		klines[i] = Kline{
			OpenTime:  c.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: c.CloseTime,
		}
	}

	return klines, nil
}

// Get retrieves market data for the specified token
func Get(symbol string) (*Data, error) {
	var klines3m, klines4h []Kline
	var err error
	// Normalize symbol
	symbol = Normalize(symbol)

	// Check if this is an xyz dex asset (use Hyperliquid API)
	isXyzAsset := IsXyzDexAsset(symbol)

	// Get 3-minute K-line data (or 5-minute for xyz assets as 3m may not be available)
	if isXyzAsset {
		// Use Hyperliquid API for xyz dex assets (use 5m since 3m may not be available)
		klines3m, err = getKlinesFromHyperliquid(symbol, "5m", 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 5-minute K-line from Hyperliquid: %v", err)
		}
	} else {
		// Use CoinAnk for regular crypto assets
		klines3m, err = getKlinesFromCoinAnk(symbol, "3m", 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 3-minute K-line from CoinAnk: %v", err)
		}
	}

	// Data staleness detection: Prevent DOGEUSDT-style price freeze issues
	if isStaleData(klines3m, symbol) {
		logger.Infof("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// Get 4-hour K-line data
	if isXyzAsset {
		klines4h, err = getKlinesFromHyperliquid(symbol, "4h", 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 4-hour K-line from Hyperliquid: %v", err)
		}
	} else {
		klines4h, err = getKlinesFromCoinAnk(symbol, "4h", 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 4-hour K-line from CoinAnk: %v", err)
		}
	}

	// Check if data is empty
	if len(klines3m) == 0 {
		return nil, fmt.Errorf("3-minute K-line data is empty")
	}
	if len(klines4h) == 0 {
		return nil, fmt.Errorf("4-hour K-line data is empty")
	}

	// Calculate current indicators (based on 3-minute latest data)
	currentPrice := klines3m[len(klines3m)-1].Close
	currentEMA20 := calculateEMA(klines3m, 20)
	currentMACD := calculateMACD(klines3m)
	currentRSI7 := calculateRSI(klines3m, 7)

	// Calculate price change percentage
	// 1-hour price change = price from 20 3-minute K-lines ago
	priceChange1h := 0.0
	if len(klines3m) >= 21 { // Need at least 21 K-lines (current + 20 previous)
		price1hAgo := klines3m[len(klines3m)-21].Close
		if price1hAgo > 0 {
			priceChange1h = ((currentPrice - price1hAgo) / price1hAgo) * 100
		}
	}

	// 4-hour price change = price from 1 4-hour K-line ago
	priceChange4h := 0.0
	if len(klines4h) >= 2 {
		price4hAgo := klines4h[len(klines4h)-2].Close
		if price4hAgo > 0 {
			priceChange4h = ((currentPrice - price4hAgo) / price4hAgo) * 100
		}
	}

	// Get OI data
	oiData, err := getOpenInterestData(symbol)
	if err != nil {
		// OI failure doesn't affect overall result, use default values
		oiData = &OIData{Latest: 0, Average: 0}
	}

	// Get Funding Rate
	fundingRate, _ := getFundingRate(symbol)

	// Calculate intraday series data
	intradayData := calculateIntradaySeries(klines3m)

	// Calculate longer-term data
	longerTermData := calculateLongerTermData(klines4h)

	return &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentMACD:       currentMACD,
		CurrentRSI7:       currentRSI7,
		OpenInterest:      oiData,
		FundingRate:       fundingRate,
		IntradaySeries:    intradayData,
		LongerTermContext: longerTermData,
	}, nil
}

// GetWithTimeframes retrieves market data for specified multiple timeframes
// This is the preferred method - uses a config struct for easy extensibility
func GetWithTimeframes(symbol string, config TimeframeFetchConfig) (*Data, error) {
	symbol = Normalize(symbol)

	if len(config.Timeframes) == 0 {
		return nil, fmt.Errorf("at least one timeframe is required")
	}

	// If primary timeframe is not specified, use the first one
	primaryTimeframe := config.PrimaryTimeframe
	if primaryTimeframe == "" {
		primaryTimeframe = config.Timeframes[0]
	}

	// Ensure primary timeframe is in the list
	hasPrimary := false
	for _, tf := range config.Timeframes {
		if tf == primaryTimeframe {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		timeframes := append([]string{primaryTimeframe}, config.Timeframes...)
		config.Timeframes = timeframes
	}

	// Store data for all timeframes
	timeframeData := make(map[string]*TimeframeSeriesData)
	var primaryKlines []Kline

	// Check if this is an xyz dex asset (use Hyperliquid API)
	isXyzAsset := IsXyzDexAsset(symbol)

	// Get K-line data for each timeframe
	for _, tf := range config.Timeframes {
		var klines []Kline
		var err error

		if isXyzAsset {
			// Use Hyperliquid API for xyz dex assets
			klines, err = getKlinesFromHyperliquid(symbol, tf, 200)
			if err != nil {
				logger.Infof("⚠️ Failed to get %s %s K-line from Hyperliquid: %v", symbol, tf, err)
				continue
			}
		} else {
			// Use CoinAnk for regular crypto assets
			klines, err = getKlinesFromCoinAnk(symbol, tf, 200)
			if err != nil {
				logger.Infof("⚠️ Failed to get %s %s K-line from CoinAnk: %v", symbol, tf, err)
				continue
			}
		}

		if len(klines) == 0 {
			logger.Infof("⚠️ %s %s K-line data is empty", symbol, tf)
			continue
		}

		// Save primary timeframe K-lines for calculating base indicators
		if tf == primaryTimeframe {
			primaryKlines = klines
		}

		// Calculate series data for this timeframe (use display count from config)
		// Pass through the ParamConfig objects
		calcConfig := IndicatorCalculationConfig{
			DisplayCount: config.DisplayCount,
			EMA:          config.EMA,
			RSI:          config.RSI,
			MACD:         config.MACD,
			ATR:          config.ATR,
			BOLL:         config.BOLL,
			BOX:          config.BOX,
		}
		seriesData := calculateTimeframeSeries(klines, tf, calcConfig)
		timeframeData[tf] = seriesData
	}

	// If primary timeframe data is empty, return error
	if len(primaryKlines) == 0 {
		return nil, fmt.Errorf("Primary timeframe %s K-line data is empty", primaryTimeframe)
	}

	// Data staleness detection
	if isStaleData(primaryKlines, symbol) {
		logger.Infof("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// Calculate current indicators (based on primary timeframe latest data)
	currentPrice := primaryKlines[len(primaryKlines)-1].Close

	// Use configured EMA period, default to 20
	emaPeriod := 20
	if config.EMA != nil && len(config.EMA.Periods) > 0 {
		emaPeriod = config.EMA.Periods[0]
	}
	currentEMA20 := calculateEMA(primaryKlines, emaPeriod)

	currentMACD := calculateMACD(primaryKlines)

	// Use configured RSI period, default to 7
	rsiPeriod := 7
	if config.RSI != nil && len(config.RSI.Periods) > 0 {
		rsiPeriod = config.RSI.Periods[0]
	}
	currentRSI7 := calculateRSI(primaryKlines, rsiPeriod)

	// Calculate price changes
	priceChange1h := calculatePriceChangeByBars(primaryKlines, primaryTimeframe, 60)  // 1 hour
	priceChange4h := calculatePriceChangeByBars(primaryKlines, primaryTimeframe, 240) // 4 hours

	// Get OI data
	oiData, err := getOpenInterestData(symbol)
	if err != nil {
		oiData = &OIData{Latest: 0, Average: 0}
	}

	// Get Funding Rate
	fundingRate, _ := getFundingRate(symbol)

	return &Data{
		Symbol:        symbol,
		CurrentPrice:  currentPrice,
		PriceChange1h: priceChange1h,
		PriceChange4h: priceChange4h,
		CurrentEMA20:  currentEMA20,
		CurrentMACD:   currentMACD,
		CurrentRSI7:   currentRSI7,
		OpenInterest:  oiData,
		FundingRate:   fundingRate,
		TimeframeData: timeframeData,
	}, nil
}

// calculateTimeframeSeries calculates series data for a single timeframe
// Uses IndicatorCalculationConfig for easy extensibility - add new parameters without changing signature
func calculateTimeframeSeries(klines []Kline, timeframe string, config IndicatorCalculationConfig) *TimeframeSeriesData {
	// Validate and set defaults
	if config.DisplayCount <= 0 {
		config.DisplayCount = 10 // default
	}

	// Extract periods from ParamConfig objects with defaults
	emaPeriods := []int{20, 50} // default
	if config.EMA != nil && len(config.EMA.Periods) > 0 {
		emaPeriods = config.EMA.Periods
	}

	rsiPeriods := []int{7, 14} // default
	if config.RSI != nil && len(config.RSI.Periods) > 0 {
		rsiPeriods = config.RSI.Periods
	}

	atrPeriods := []int{14} // default
	if config.ATR != nil && len(config.ATR.Periods) > 0 {
		atrPeriods = config.ATR.Periods
	}

	bollPeriods := []int{20} // default
	if config.BOLL != nil && len(config.BOLL.Periods) > 0 {
		bollPeriods = config.BOLL.Periods
	}

	bollStdDev := 2.0 // default
	if config.BOLL != nil && config.BOLL.StdDevMultiplier > 0 {
		bollStdDev = config.BOLL.StdDevMultiplier
	}

	boxRatio := 1.03 // default
	if config.BOX != nil && config.BOX.Ratio > 1.0 {
		boxRatio = config.BOX.Ratio
	}

	data := &TimeframeSeriesData{
		Timeframe:   timeframe,
		Klines:      make([]KlineBar, 0, config.DisplayCount),
		MidPrices:   make([]float64, 0, config.DisplayCount),
		EMA20Values: make([]float64, 0, config.DisplayCount),
		EMA50Values: make([]float64, 0, config.DisplayCount),
		MACDValues:  make([]float64, 0, config.DisplayCount),
		RSI7Values:  make([]float64, 0, config.DisplayCount),
		RSI14Values: make([]float64, 0, config.DisplayCount),
		Volume:      make([]float64, 0, config.DisplayCount),
		BOLLUpper:   make([]float64, 0, config.DisplayCount),
		BOLLMiddle:  make([]float64, 0, config.DisplayCount),
		BOLLLower:   make([]float64, 0, config.DisplayCount),
	}

	// Get latest N data points based on display count from config
	start := len(klines) - config.DisplayCount
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		// Store full OHLCV kline data
		data.Klines = append(data.Klines, KlineBar{
			Time:   klines[i].OpenTime,
			Open:   klines[i].Open,
			High:   klines[i].High,
			Low:    klines[i].Low,
			Close:  klines[i].Close,
			Volume: klines[i].Volume,
		})

		// Keep MidPrices and Volume for backward compatibility
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA for each configured period
		// First period goes to EMA20Values, second to EMA50Values (for backward compatibility)
		if len(emaPeriods) > 0 {
			period1 := emaPeriods[0]
			if i >= period1-1 {
				ema := calculateEMA(klines[:i+1], period1)
				data.EMA20Values = append(data.EMA20Values, ema)
			}
		}
		if len(emaPeriods) > 1 {
			period2 := emaPeriods[1]
			if i >= period2-1 {
				ema := calculateEMA(klines[:i+1], period2)
				data.EMA50Values = append(data.EMA50Values, ema)
			}
		}

		// Calculate MACD for each point (fixed parameters)
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each configured period
		// First period goes to RSI7Values, second to RSI14Values (for backward compatibility)
		if len(rsiPeriods) > 0 {
			period1 := rsiPeriods[0]
			if i >= period1-1 {
				rsi := calculateRSI(klines[:i+1], period1)
				data.RSI7Values = append(data.RSI7Values, rsi)
			}
		}
		if len(rsiPeriods) > 1 {
			period2 := rsiPeriods[1]
			if i >= period2-1 {
				rsi := calculateRSI(klines[:i+1], period2)
				data.RSI14Values = append(data.RSI14Values, rsi)
			}
		}

		// Calculate Bollinger Bands for first configured period
		if len(bollPeriods) > 0 {
			period := bollPeriods[0]
			if i >= period-1 {
				upper, middle, lower := calculateBOLL(klines[:i+1], period, bollStdDev)
				data.BOLLUpper = append(data.BOLLUpper, upper)
				data.BOLLMiddle = append(data.BOLLMiddle, middle)
				data.BOLLLower = append(data.BOLLLower, lower)
			}
		}
	}

	// Calculate ATR for first configured period (stored in ATR14 field for backward compatibility)
	if len(atrPeriods) > 0 {
		data.ATR14 = calculateATR(klines, atrPeriods[0])
	}

	// Calculate Expectation Box (tops/bottoms based support/resistance)
	// BOX uses ALL available klines, not just the display count
	// This ensures we have enough data to detect meaningful tops/bottoms (need at least 3 tops and 3 bottoms)
	// ratio must be > 1, where 1.03 = 3% price movement required to confirm a top/bottom
	boxTop, boxBottom := calculateExpectationBox(klines, boxRatio, 0) // 0 = unlimited lookback
	if len(boxTop) == 2 && len(boxBottom) == 2 {
		data.BOXTop = boxTop
		data.BOXBottom = boxBottom
	}

	return data
}

// calculatePriceChangeByBars calculates how many K-lines to look back for price change based on timeframe
func calculatePriceChangeByBars(klines []Kline, timeframe string, targetMinutes int) float64 {
	if len(klines) < 2 {
		return 0
	}

	// Parse timeframe to minutes
	tfMinutes := parseTimeframeToMinutes(timeframe)
	if tfMinutes <= 0 {
		return 0
	}

	// Calculate how many K-lines to look back
	barsBack := targetMinutes / tfMinutes
	if barsBack < 1 {
		barsBack = 1
	}

	currentPrice := klines[len(klines)-1].Close
	idx := len(klines) - 1 - barsBack
	if idx < 0 {
		idx = 0
	}

	oldPrice := klines[idx].Close
	if oldPrice > 0 {
		return ((currentPrice - oldPrice) / oldPrice) * 100
	}
	return 0
}

// parseTimeframeToMinutes parses timeframe string to minutes
func parseTimeframeToMinutes(tf string) int {
	switch tf {
	case "1m":
		return 1
	case "3m":
		return 3
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "6h":
		return 360
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "3d":
		return 4320
	case "1w":
		return 10080
	default:
		return 0
	}
}

// calculateEMA calculates EMA
func calculateEMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	// Calculate SMA as initial EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	// Calculate EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

// calculateMACD calculates MACD
func calculateMACD(klines []Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	// Calculate 12-period and 26-period EMA
	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	// MACD = EMA12 - EMA26
	return ema12 - ema26
}

// calculateRSI calculates RSI
func calculateRSI(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	// Calculate initial average gain/loss
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Use Wilder smoothing method to calculate subsequent RSI
	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateATR calculates ATR
func calculateATR(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// Calculate initial ATR
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder smoothing
	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateBOLL calculates Bollinger Bands (upper, middle, lower)
// period: typically 20, multiplier: typically 2
func calculateBOLL(klines []Kline, period int, multiplier float64) (upper, middle, lower float64) {
	if len(klines) < period {
		return 0, 0, 0
	}

	// Calculate SMA (middle band)
	sum := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		sum += klines[i].Close
	}
	sma := sum / float64(period)

	// Calculate standard deviation
	variance := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		diff := klines[i].Close - sma
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	// Calculate bands
	middle = sma
	upper = sma + multiplier*stdDev
	lower = sma - multiplier*stdDev

	return upper, middle, lower
}

// calculateIntradaySeries calculates intraday series data
func calculateIntradaySeries(klines []Kline) *IntradayData {
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		MACDValues:  make([]float64, 0, 10),
		RSI7Values:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
		Volume:      make([]float64, 0, 10),
	}

	// Get latest 10 data points
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	// Calculate 3m ATR14
	data.ATR14 = calculateATR(klines, 14)

	return data
}

// calculateLongerTermData calculates longer-term data
func calculateLongerTermData(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Calculate EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// Calculate ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// Calculate volume
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// Calculate average volume
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// Calculate MACD and RSI series
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// getOpenInterestData retrieves OI data
func getOpenInterestData(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/openInterest?symbol=%s", symbol)

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	oi, _ := strconv.ParseFloat(result.OpenInterest, 64)

	return &OIData{
		Latest:  oi,
		Average: oi * 0.999, // Approximate average
	}, nil
}

// getFundingRate retrieves funding rate (optimized: uses 1-hour cache)
func getFundingRate(symbol string) (float64, error) {
	// Check cache (1-hour validity)
	// Funding Rate only updates every 8 hours, 1-hour cache is very reasonable
	if cached, ok := fundingRateMap.Load(symbol); ok {
		cache := cached.(*FundingRateCache)
		if time.Since(cache.UpdatedAt) < frCacheTTL {
			// Cache hit, return directly
			return cache.Rate, nil
		}
	}

	// Cache expired or doesn't exist, call API
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		IndexPrice      string `json:"indexPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		InterestRate    string `json:"interestRate"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	rate, _ := strconv.ParseFloat(result.LastFundingRate, 64)

	// Update cache
	fundingRateMap.Store(symbol, &FundingRateCache{
		Rate:      rate,
		UpdatedAt: time.Now(),
	})

	return rate, nil
}

// Format formats and outputs market data
func Format(data *Data) string {
	var sb strings.Builder

	// Format price with dynamic precision
	priceStr := formatPriceWithDynamicPrecision(data.CurrentPrice)
	sb.WriteString(fmt.Sprintf("current_price = %s, current_ema20 = %.3f, current_macd = %.3f, current_rsi (7 period) = %.3f\n\n",
		priceStr, data.CurrentEMA20, data.CurrentMACD, data.CurrentRSI7))

	sb.WriteString(fmt.Sprintf("In addition, here is the latest %s open interest and funding rate for perps:\n\n",
		data.Symbol))

	if data.OpenInterest != nil {
		// Format OI data with dynamic precision
		oiLatestStr := formatPriceWithDynamicPrecision(data.OpenInterest.Latest)
		oiAverageStr := formatPriceWithDynamicPrecision(data.OpenInterest.Average)
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %s Average: %s\n\n",
			oiLatestStr, oiAverageStr))
	}

	sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (3‑minute intervals, oldest → latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
		}

		if len(data.IntradaySeries.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
		}

		sb.WriteString(fmt.Sprintf("3m ATR (14‑period): %.3f\n\n", data.IntradaySeries.ATR14))
	}

	if data.LongerTermContext != nil {
		sb.WriteString("Longer‑term context (4‑hour timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
		}

		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
		}
	}

	// Multi-timeframe data (new)
	if len(data.TimeframeData) > 0 {
		// Output sorted by timeframe
		timeframeOrder := []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
		for _, tf := range timeframeOrder {
			if tfData, ok := data.TimeframeData[tf]; ok {
				sb.WriteString(fmt.Sprintf("=== %s Timeframe ===\n\n", strings.ToUpper(tf)))
				formatTimeframeData(&sb, tfData)
			}
		}
	}

	return sb.String()
}

// formatTimeframeData formats data for a single timeframe
func formatTimeframeData(sb *strings.Builder, data *TimeframeSeriesData) {
	// Use OHLCV table format if kline data is available
	if len(data.Klines) > 0 {
		sb.WriteString("Time(UTC)      Open      High      Low       Close     Volume\n")
		for i, k := range data.Klines {
			t := time.Unix(k.Time/1000, 0).UTC()
			timeStr := t.Format("01-02 15:04")
			marker := ""
			if i == len(data.Klines)-1 {
				marker = "  <- current"
			}
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f %-12.2f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, k.Volume, marker))
		}
		sb.WriteString("\n")
	} else if len(data.MidPrices) > 0 {
		// Fallback to old format for backward compatibility
		sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.MidPrices)))
		if len(data.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.Volume)))
		}
	}

	// Technical indicators
	if len(data.EMA20Values) > 0 {
		sb.WriteString(fmt.Sprintf("EMA20: %s\n", formatFloatSlice(data.EMA20Values)))
	}

	if len(data.EMA50Values) > 0 {
		sb.WriteString(fmt.Sprintf("EMA50: %s\n", formatFloatSlice(data.EMA50Values)))
	}

	if len(data.MACDValues) > 0 {
		sb.WriteString(fmt.Sprintf("MACD: %s\n", formatFloatSlice(data.MACDValues)))
	}

	if len(data.RSI7Values) > 0 {
		sb.WriteString(fmt.Sprintf("RSI7: %s\n", formatFloatSlice(data.RSI7Values)))
	}

	if len(data.RSI14Values) > 0 {
		sb.WriteString(fmt.Sprintf("RSI14: %s\n", formatFloatSlice(data.RSI14Values)))
	}

	if data.ATR14 > 0 {
		sb.WriteString(fmt.Sprintf("ATR14: %.4f\n", data.ATR14))
	}

	sb.WriteString("\n")
}

// formatPriceWithDynamicPrecision dynamically selects precision based on price range
// This perfectly supports all coins from ultra-low price meme coins (< 0.0001) to BTC/ETH
func formatPriceWithDynamicPrecision(price float64) string {
	switch {
	case price < 0.0001:
		// Ultra-low price meme coins: 1000SATS, 1000WHY, DOGS
		// 0.00002070 → "0.00002070" (8 decimal places)
		return fmt.Sprintf("%.8f", price)
	case price < 0.001:
		// Low price meme coins: NEIRO, HMSTR, HOT, NOT
		// 0.00015060 → "0.000151" (6 decimal places)
		return fmt.Sprintf("%.6f", price)
	case price < 0.01:
		// Mid-low price coins: PEPE, SHIB, MEME
		// 0.00556800 → "0.005568" (6 decimal places)
		return fmt.Sprintf("%.6f", price)
	case price < 1.0:
		// Low price coins: ASTER, DOGE, ADA, TRX
		// 0.9954 → "0.9954" (4 decimal places)
		return fmt.Sprintf("%.4f", price)
	case price < 100:
		// Mid price coins: SOL, AVAX, LINK, MATIC
		// 23.4567 → "23.4567" (4 decimal places)
		return fmt.Sprintf("%.4f", price)
	default:
		// High price coins: BTC, ETH (save tokens)
		// 45678.9123 → "45678.91" (2 decimal places)
		return fmt.Sprintf("%.2f", price)
	}
}

// formatFloatSlice formats float64 slice to string (using dynamic precision)
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = formatPriceWithDynamicPrecision(v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// xyz dex assets that should NOT get USDT suffix
var xyzDexAssets = map[string]bool{
	// Stocks
	"TSLA": true, "NVDA": true, "AAPL": true, "MSFT": true, "META": true,
	"AMZN": true, "GOOGL": true, "AMD": true, "COIN": true, "NFLX": true,
	"PLTR": true, "HOOD": true, "INTC": true, "MSTR": true, "TSM": true,
	"ORCL": true, "MU": true, "RIVN": true, "COST": true, "LLY": true,
	"CRCL": true, "SKHX": true, "SNDK": true,
	// Forex
	"EUR": true, "JPY": true,
	// Commodities
	"GOLD": true, "SILVER": true,
	// Index
	"XYZ100": true,
}

// IsXyzDexAsset checks if a symbol is an xyz dex asset
func IsXyzDexAsset(symbol string) bool {
	base := strings.ToUpper(symbol)
	// Remove any prefix/suffix
	base = strings.TrimPrefix(base, "XYZ:")
	for _, suffix := range []string{"USDT", "USD", "-USDC"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return xyzDexAssets[base]
}

// Normalize normalizes symbol
// For crypto: ensures it's a USDT trading pair
// For xyz dex assets (stocks, forex, commodities): uses xyz: prefix without USDT suffix
func Normalize(symbol string) string {
	symbol = strings.ToUpper(symbol)

	// Check if this is an xyz dex asset
	if IsXyzDexAsset(symbol) {
		// Remove any xyz: prefix (case-insensitive) and USDT suffix, then add xyz: prefix
		base := symbol
		// Handle both lowercase and uppercase xyz: prefix
		if strings.HasPrefix(strings.ToLower(base), "xyz:") {
			base = base[4:] // Remove first 4 characters ("xyz:")
		}
		for _, suffix := range []string{"USDT", "USD", "-USDC"} {
			if strings.HasSuffix(base, suffix) {
				base = strings.TrimSuffix(base, suffix)
				break
			}
		}
		return "xyz:" + base
	}

	// For regular crypto assets
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// parseFloat parses float value
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// BuildDataFromKlines constructs market data snapshot from preloaded K-line series (for backtesting/simulation).
func BuildDataFromKlines(symbol string, primary []Kline, longer []Kline) (*Data, error) {
	if len(primary) == 0 {
		return nil, fmt.Errorf("primary series is empty")
	}

	symbol = Normalize(symbol)
	current := primary[len(primary)-1]
	currentPrice := current.Close

	data := &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		CurrentEMA20:      calculateEMA(primary, 20),
		CurrentMACD:       calculateMACD(primary),
		CurrentRSI7:       calculateRSI(primary, 7),
		PriceChange1h:     priceChangeFromSeries(primary, time.Hour),
		PriceChange4h:     priceChangeFromSeries(primary, 4*time.Hour),
		OpenInterest:      &OIData{Latest: 0, Average: 0},
		FundingRate:       0,
		IntradaySeries:    calculateIntradaySeries(primary),
		LongerTermContext: nil,
	}

	if len(longer) > 0 {
		data.LongerTermContext = calculateLongerTermData(longer)
	}

	return data, nil
}

func priceChangeFromSeries(series []Kline, duration time.Duration) float64 {
	if len(series) == 0 || duration <= 0 {
		return 0
	}
	last := series[len(series)-1]
	target := last.CloseTime - duration.Milliseconds()
	for i := len(series) - 1; i >= 0; i-- {
		if series[i].CloseTime <= target {
			price := series[i].Close
			if price > 0 {
				return ((last.Close - price) / price) * 100
			}
			break
		}
	}
	return 0
}

// isStaleData detects stale data (consecutive price freeze)
// Fix DOGEUSDT-style issue: consecutive N periods with completely unchanged prices indicate data source anomaly
func isStaleData(klines []Kline, symbol string) bool {
	if len(klines) < 5 {
		return false // Insufficient data to determine
	}

	// Detection threshold: 5 consecutive 3-minute periods with unchanged price (15 minutes without fluctuation)
	const stalePriceThreshold = 5
	const priceTolerancePct = 0.0001 // 0.01% fluctuation tolerance (avoid false positives)

	// Take the last stalePriceThreshold K-lines
	recentKlines := klines[len(klines)-stalePriceThreshold:]
	firstPrice := recentKlines[0].Close

	// Check if all prices are within tolerance
	for i := 1; i < len(recentKlines); i++ {
		priceDiff := math.Abs(recentKlines[i].Close-firstPrice) / firstPrice
		if priceDiff > priceTolerancePct {
			return false // Price fluctuation exists, data is normal
		}
	}

	// Additional check: MACD and volume
	// If price is unchanged but MACD/volume shows normal fluctuation, it might be a real market situation (extremely low volatility)
	// Check if volume is also 0 (data completely frozen)
	allVolumeZero := true
	for _, k := range recentKlines {
		if k.Volume > 0 {
			allVolumeZero = false
			break
		}
	}

	if allVolumeZero {
		logger.Infof("⚠️  %s stale data confirmed: price freeze + zero volume", symbol)
		return true
	}

	// Price frozen but has volume: might be extremely low volatility market, allow but log warning
	logger.Infof("⚠️  %s detected extreme price stability (no fluctuation for %d consecutive periods), but volume is normal", symbol, stalePriceThreshold)
	return false
}

// ========== 导出的指标计算函数（供测试使用） ==========

// ExportCalculateEMA exports calculateEMA for testing
func ExportCalculateEMA(klines []Kline, period int) float64 {
	return calculateEMA(klines, period)
}

// ExportCalculateMACD exports calculateMACD for testing
func ExportCalculateMACD(klines []Kline) float64 {
	return calculateMACD(klines)
}

// ExportCalculateRSI exports calculateRSI for testing
func ExportCalculateRSI(klines []Kline, period int) float64 {
	return calculateRSI(klines, period)
}

// ExportCalculateATR exports calculateATR for testing
func ExportCalculateATR(klines []Kline, period int) float64 {
	return calculateATR(klines, period)
}

// ExportCalculateBOLL exports calculateBOLL for testing
func ExportCalculateBOLL(klines []Kline, period int, multiplier float64) (upper, middle, lower float64) {
	return calculateBOLL(klines, period, multiplier)
}

// calculateDonchian calculates Donchian channel (highest high, lowest low) for given period
func calculateDonchian(klines []Kline, period int) (upper, lower float64) {
	if len(klines) == 0 || period <= 0 {
		return 0, 0
	}

	// Use all available klines if period > len(klines)
	start := len(klines) - period
	if start < 0 {
		start = 0
	}

	upper = klines[start].High
	lower = klines[start].Low

	for i := start + 1; i < len(klines); i++ {
		if klines[i].High > upper {
			upper = klines[i].High
		}
		if klines[i].Low < lower {
			lower = klines[i].Low
		}
	}

	return upper, lower
}

// ExportCalculateDonchian exports calculateDonchian for testing
func ExportCalculateDonchian(klines []Kline, period int) (float64, float64) {
	return calculateDonchian(klines, period)
}

// Box period constants (in 1h candles)
const (
	ShortBoxPeriod = 72  // 3 days of 1h candles
	MidBoxPeriod   = 240 // 10 days of 1h candles
	LongBoxPeriod  = 500 // ~21 days of 1h candles
)

// calculateBoxData calculates multi-period box data from klines
func calculateBoxData(klines []Kline, currentPrice float64) *BoxData {
	box := &BoxData{
		CurrentPrice: currentPrice,
	}

	if len(klines) == 0 {
		return box
	}

	box.ShortUpper, box.ShortLower = calculateDonchian(klines, ShortBoxPeriod)
	box.MidUpper, box.MidLower = calculateDonchian(klines, MidBoxPeriod)
	box.LongUpper, box.LongLower = calculateDonchian(klines, LongBoxPeriod)

	return box
}

// ExportCalculateBoxData exports calculateBoxData for testing
func ExportCalculateBoxData(klines []Kline, currentPrice float64) *BoxData {
	return calculateBoxData(klines, currentPrice)
}

// GetBoxData fetches 1h klines and calculates box data for a symbol
func GetBoxData(symbol string) (*BoxData, error) {
	symbol = Normalize(symbol)

	// Fetch 500 1h klines
	var klines []Kline
	var err error

	if IsXyzDexAsset(symbol) {
		klines, err = getKlinesFromHyperliquid(symbol, "1h", LongBoxPeriod)
	} else {
		klines, err = getKlinesFromCoinAnk(symbol, "1h", LongBoxPeriod)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get 1h klines: %w", err)
	}

	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data available")
	}

	currentPrice := klines[len(klines)-1].Close

	return calculateBoxData(klines, currentPrice), nil
}

// TopBottom represents a single top or bottom point
type TopBottom struct {
	Index int     // Index in the kline array
	Price float64 // Price at this point
	Time  int64   // Timestamp
}

// findTopsAndBottoms finds alternating tops and bottoms based on price movement ratio
// Algorithm: Search for local extrema where price moves by ratio in opposite direction
// This matches the Python implementation in qi_consultant/tradingbox/best_signal.py
func findTopsAndBottoms(klines []Kline, ratio float64) ([]TopBottom, []TopBottom) {
	var tops []TopBottom
	var bottoms []TopBottom

	if len(klines) < 3 || ratio <= 1 {
		return tops, bottoms
	}

	// Start by finding the first top
	lookingForTop := true
	prevExtremeIndex := 0

	for i := 1; i < len(klines); i++ {
		currentPrice := klines[i].Close
		prevExtremePrice := klines[prevExtremeIndex].Close

		if lookingForTop {
			// Update the top if current price is higher
			if currentPrice > prevExtremePrice {
				prevExtremeIndex = i
				prevExtremePrice = currentPrice
			} else if currentPrice < prevExtremePrice/ratio {
				// Switch to finding bottom if price ratio condition is met
				// Python: elif curr_price < prev_price / ratio:
				tops = append(tops, TopBottom{
					Index: prevExtremeIndex,
					Price: prevExtremePrice,
					Time:  klines[prevExtremeIndex].OpenTime,
				})
				prevExtremeIndex = i
				lookingForTop = false
			}
		} else {
			// Update the bottom if current price is lower
			if currentPrice < prevExtremePrice {
				prevExtremeIndex = i
				prevExtremePrice = currentPrice
			} else if currentPrice > prevExtremePrice*ratio {
				// Switch to finding top if price ratio condition is met
				// Python: elif curr_price > prev_price * ratio:
				bottoms = append(bottoms, TopBottom{
					Index: prevExtremeIndex,
					Price: prevExtremePrice,
					Time:  klines[prevExtremeIndex].OpenTime,
				})
				prevExtremeIndex = i
				lookingForTop = true
			}
		}
	}

	// Add the last identified extreme point if not already included
	// Python: if prev_extreme_index not in [entry['Date'] for entry in result]:
	lastIndex := -1
	if lookingForTop {
		// Check if this index is already in tops
		for _, t := range tops {
			if t.Index == prevExtremeIndex {
				lastIndex = t.Index
				break
			}
		}
	} else {
		// Check if this index is already in bottoms
		for _, b := range bottoms {
			if b.Index == prevExtremeIndex {
				lastIndex = b.Index
				break
			}
		}
	}

	if lastIndex != prevExtremeIndex {
		if lookingForTop {
			tops = append(tops, TopBottom{
				Index: prevExtremeIndex,
				Price: klines[prevExtremeIndex].Close,
				Time:  klines[prevExtremeIndex].OpenTime,
			})
		} else {
			bottoms = append(bottoms, TopBottom{
				Index: prevExtremeIndex,
				Price: klines[prevExtremeIndex].Close,
				Time:  klines[prevExtremeIndex].OpenTime,
			})
		}
	}

	return tops, bottoms
}

// calculateExpectationBox calculates tops/bottoms based box data (from get_expectation logic)
// Returns BOXTop and BOXBottom arrays where:
//   - Index 0 = second-to-last top/bottom (for stop-loss)
//   - Index 1 = last top/bottom (for current support/resistance)
// ratio: Price movement ratio to confirm top/bottom (e.g. 1.03 = 3%)
// lookback: Max number of periods to search (0 = unlimited, uses all klines)
func calculateExpectationBox(klines []Kline, ratio float64, lookback int) (boxTop []float64, boxBottom []float64) {
	// Apply lookback limit if specified (0 = unlimited)
	searchKlines := klines
	if lookback > 0 && len(klines) > lookback {
		searchKlines = klines[len(klines)-lookback:]
	}

	// Find tops and bottoms
	tops, bottoms := findTopsAndBottoms(searchKlines, ratio)

	// Need at least 3 tops and 3 bottoms for meaningful analysis
	if len(tops) < 2 || len(bottoms) < 2 {
		return nil, nil
	}

	currentPrice := klines[len(klines)-1].Close

	// Get last 6 tops and bottoms (most recent)
	// Use max to avoid negative slice indices when len < 6
	topStart := len(tops) - 6
	if topStart < 0 {
		topStart = 0
	}
	bottomStart := len(bottoms) - 6
	if bottomStart < 0 {
		bottomStart = 0
	}
	lastTops := tops[topStart:]
	lastBottoms := bottoms[bottomStart:]

	// Filter tops above current price and bottoms below current price
	var relevantTops []TopBottom
	var relevantBottoms []TopBottom

	for _, top := range lastTops {
		if top.Price >= currentPrice {
			relevantTops = append(relevantTops, top)
		}
	}
	for _, bottom := range lastBottoms {
		if bottom.Price <= currentPrice {
			relevantBottoms = append(relevantBottoms, bottom)
		}
	}

	if len(relevantTops) < 2 || len(relevantBottoms) < 2 {
		return nil, nil
	}

	// Get last and second-to-last tops and bottoms
	secondToLastTop := relevantTops[len(relevantTops)-2]
	lastTop := relevantTops[len(relevantTops)-1]
	secondToLastBottom := relevantBottoms[len(relevantBottoms)-2]
	lastBottom := relevantBottoms[len(relevantBottoms)-1]

	// Build result arrays: [second-to-last, last]
	boxTop = []float64{secondToLastTop.Price, lastTop.Price}
	boxBottom = []float64{secondToLastBottom.Price, lastBottom.Price}

	return boxTop, boxBottom
}

// ExportCalculateExpectationBox exports calculateExpectationBox for testing
// lookback: Max number of periods to search (0 = unlimited, uses all klines)
func ExportCalculateExpectationBox(klines []Kline, ratio float64, lookback int) ([]float64, []float64) {
	return calculateExpectationBox(klines, ratio, lookback)
}
