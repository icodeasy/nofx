package types

// Indicator Parameter Configuration Types
// These are shared between store and market packages to avoid duplication

// EMAParamConfig EMA indicator parameter configuration
type EMAParamConfig struct {
	Periods []int `json:"periods,omitempty"` // EMA periods, e.g. [20, 50, 200]
}

// RSIParamConfig RSI indicator parameter configuration
type RSIParamConfig struct {
	Periods []int `json:"periods,omitempty"` // RSI periods, e.g. [7, 14]
}

// MACDParamConfig MACD indicator parameter configuration
type MACDParamConfig struct {
	FastPeriod   int `json:"fast_period,omitempty"`   // Fast EMA period (default: 12)
	SlowPeriod   int `json:"slow_period,omitempty"`   // Slow EMA period (default: 26)
	SignalPeriod int `json:"signal_period,omitempty"` // Signal line period (default: 9)
}

// ATRParamConfig ATR indicator parameter configuration
type ATRParamConfig struct {
	Periods []int `json:"periods,omitempty"` // ATR periods, e.g. [14]
}

// BOLLParamConfig Bollinger Bands parameter configuration
type BOLLParamConfig struct {
	Periods          []int   `json:"periods,omitempty"`           // BOLL periods, e.g. [20]
	StdDevMultiplier float64 `json:"std_dev_multiplier,omitempty"` // Standard deviation multiplier (default: 2.0)
}

// BOXParamConfig Expectation Box parameter configuration
type BOXParamConfig struct {
	Ratio      float64 `json:"ratio,omitempty"`       // Price movement ratio (default: 1.03 = 3%)
	KlineCount int     `json:"kline_count,omitempty"` // Number of klines to fetch for calculation (default: 1000)
}
