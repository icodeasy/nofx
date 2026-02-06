package store

import (
	"encoding/json"
	"testing"

	"nofx/types"

	"github.com/stretchr/testify/assert"
)

func TestIndicatorConfigJSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		input    IndicatorConfig
		expected string
	}{
		{
			name: "Serialize new structured format",
			input: IndicatorConfig{
				EMA:  &types.EMAParamConfig{Periods: []int{20, 50}},
				RSI:  &types.RSIParamConfig{Periods: []int{7, 14}},
				ATR:  &types.ATRParamConfig{Periods: []int{14}},
				BOLL: &types.BOLLParamConfig{Periods: []int{20}, StdDevMultiplier: 2.0},
				BOX:  &types.BOXParamConfig{Ratio: 1.03},
			},
			expected: `{"ema":{"periods":[20,50]},"rsi":{"periods":[7,14]},"atr":{"periods":[14]},"boll":{"periods":[20],"std_dev_multiplier":2},"box":{"ratio":1.03}}`,
		},
		{
			name: "Serialize old format (for backward compatibility)",
			input: IndicatorConfig{
				EMAPeriods:  []int{20, 50},
				RSIPeriods:  []int{7, 14},
				ATRPeriods:  []int{14},
				BOLLPeriods: []int{20},
				BOXRatio:    1.03,
			},
			expected: `{"ema_periods":[20,50],"rsi_periods":[7,14],"atr_periods":[14],"boll_periods":[20],"box_ratio":1.03}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize to JSON
			data, err := json.Marshal(tt.input)
			assert.NoError(t, err)

			// Verify JSON contains expected fields (flexible matching for order)
			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			assert.NoError(t, err)

			var expected map[string]interface{}
			err = json.Unmarshal([]byte(tt.expected), &expected)
			assert.NoError(t, err)

			// Check that all expected keys exist
			for key := range expected {
				assert.Contains(t, result, key, "Missing key: %s", key)
			}

			t.Logf("Serialized JSON: %s", string(data))
		})
	}
}

func TestIndicatorConfigJSONDeserialization(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		check func(*testing.T, *IndicatorConfig)
	}{
		{
			name: "Deserialize new structured format",
			json: `{"ema":{"periods":[20,50]},"rsi":{"periods":[7,14]},"box":{"ratio":1.05}}`,
			check: func(t *testing.T, config *IndicatorConfig) {
				assert.NotNil(t, config.EMA)
				assert.Equal(t, []int{20, 50}, config.EMA.Periods)
				assert.NotNil(t, config.RSI)
				assert.Equal(t, []int{7, 14}, config.RSI.Periods)
				assert.NotNil(t, config.BOX)
				assert.Equal(t, 1.05, config.BOX.Ratio)
			},
		},
		{
			name: "Deserialize old format and migrate",
			json: `{"ema_periods":[9,20],"rsi_periods":[14],"box_ratio":1.04}`,
			check: func(t *testing.T, config *IndicatorConfig) {
				// Before migration, old fields are populated
				assert.Equal(t, []int{9, 20}, config.EMAPeriods)
				assert.Equal(t, []int{14}, config.RSIPeriods)
				assert.Equal(t, 1.04, config.BOXRatio)

				// After migration, old fields are cleared and new fields are populated
				config.MigrateIndicatorConfig()
				assert.Nil(t, config.EMAPeriods)
				assert.Nil(t, config.RSIPeriods)
				assert.Equal(t, 0.0, config.BOXRatio)
				assert.NotNil(t, config.EMA)
				assert.Equal(t, []int{9, 20}, config.EMA.Periods)
				assert.NotNil(t, config.RSI)
				assert.Equal(t, []int{14}, config.RSI.Periods)
				assert.NotNil(t, config.BOX)
				assert.Equal(t, 1.04, config.BOX.Ratio)
			},
		},
		{
			name: "Deserialize empty config",
			json: `{}`,
			check: func(t *testing.T, config *IndicatorConfig) {
				// All getters should return defaults
				emaConfig := config.GetEMAParamConfig()
				assert.Equal(t, []int{20, 50}, emaConfig.Periods)

				boxConfig := config.GetBOXParamConfig()
				assert.Equal(t, 1.03, boxConfig.Ratio)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config IndicatorConfig
			err := json.Unmarshal([]byte(tt.json), &config)
			assert.NoError(t, err)

			tt.check(t, &config)
		})
	}
}

func TestStrategyConfigRoundTrip(t *testing.T) {
	original := &StrategyConfig{
		Indicators: IndicatorConfig{
			EMA:  &types.EMAParamConfig{Periods: []int{9, 20, 50}},
			RSI:  &types.RSIParamConfig{Periods: []int{7}},
			ATR:  &types.ATRParamConfig{Periods: []int{14}},
			BOLL: &types.BOLLParamConfig{Periods: []int{20}, StdDevMultiplier: 2.5},
			BOX:  &types.BOXParamConfig{Ratio: 1.05},
			MACD: &types.MACDParamConfig{FastPeriod: 10, SlowPeriod: 24, SignalPeriod: 8},
		},
	}

	// Serialize to JSON (simulating database storage)
	data, err := json.Marshal(original)
	assert.NoError(t, err)
	t.Logf("Serialized StrategyConfig: %s", string(data))

	// Deserialize from JSON (simulating database retrieval)
	var retrieved StrategyConfig
	err = json.Unmarshal(data, &retrieved)
	assert.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, original.Indicators.EMA.Periods, retrieved.Indicators.EMA.Periods)
	assert.Equal(t, original.Indicators.RSI.Periods, retrieved.Indicators.RSI.Periods)
	assert.Equal(t, original.Indicators.ATR.Periods, retrieved.Indicators.ATR.Periods)
	assert.Equal(t, original.Indicators.BOLL.Periods, retrieved.Indicators.BOLL.Periods)
	assert.Equal(t, original.Indicators.BOLL.StdDevMultiplier, retrieved.Indicators.BOLL.StdDevMultiplier)
	assert.Equal(t, original.Indicators.BOX.Ratio, retrieved.Indicators.BOX.Ratio)
	assert.Equal(t, original.Indicators.MACD.FastPeriod, retrieved.Indicators.MACD.FastPeriod)
	assert.Equal(t, original.Indicators.MACD.SlowPeriod, retrieved.Indicators.MACD.SlowPeriod)
	assert.Equal(t, original.Indicators.MACD.SignalPeriod, retrieved.Indicators.MACD.SignalPeriod)
}
