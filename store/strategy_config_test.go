package store

import (
	"testing"

	"nofx/types"

	"github.com/stretchr/testify/assert"
)

func TestIndicatorConfigMigration(t *testing.T) {
	tests := []struct {
		name     string
		input    IndicatorConfig
		expected IndicatorConfig
	}{
		{
			name: "Migrate old EMA periods to new format",
			input: IndicatorConfig{
				EMAPeriods: []int{20, 50},
			},
			expected: IndicatorConfig{
				EMA: &types.EMAParamConfig{Periods: []int{20, 50}},
			},
		},
		{
			name: "Migrate old RSI periods to new format",
			input: IndicatorConfig{
				RSIPeriods: []int{7, 14},
			},
			expected: IndicatorConfig{
				RSI: &types.RSIParamConfig{Periods: []int{7, 14}},
			},
		},
		{
			name: "Migrate old ATR periods to new format",
			input: IndicatorConfig{
				ATRPeriods: []int{14},
			},
			expected: IndicatorConfig{
				ATR: &types.ATRParamConfig{Periods: []int{14}},
			},
		},
		{
			name: "Migrate old BOLL periods to new format",
			input: IndicatorConfig{
				BOLLPeriods: []int{20},
			},
			expected: IndicatorConfig{
				BOLL: &types.BOLLParamConfig{
					Periods:          []int{20},
					StdDevMultiplier: 2.0,
				},
			},
		},
		{
			name: "Migrate old BOX ratio to new format",
			input: IndicatorConfig{
				BOXRatio: 1.05,
			},
			expected: IndicatorConfig{
				BOX: &types.BOXParamConfig{Ratio: 1.05},
			},
		},
		{
			name: "Migrate all old params at once",
			input: IndicatorConfig{
				EMAPeriods:  []int{9, 20},
				RSIPeriods:  []int{14},
				ATRPeriods:  []int{14},
				BOLLPeriods: []int{20},
				BOXRatio:    1.03,
			},
			expected: IndicatorConfig{
				EMA: &types.EMAParamConfig{Periods: []int{9, 20}},
				RSI: &types.RSIParamConfig{Periods: []int{14}},
				ATR: &types.ATRParamConfig{Periods: []int{14}},
				BOLL: &types.BOLLParamConfig{
					Periods:          []int{20},
					StdDevMultiplier: 2.0,
				},
				BOX: &types.BOXParamConfig{Ratio: 1.03},
			},
		},
		{
			name: "Already in new format - no migration needed",
			input: IndicatorConfig{
				EMA: &types.EMAParamConfig{Periods: []int{20, 50}},
			},
			expected: IndicatorConfig{
				EMA: &types.EMAParamConfig{Periods: []int{20, 50}},
			},
		},
		{
			name:     "Empty config - use defaults",
			input:    IndicatorConfig{},
			expected: IndicatorConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call migration
			tt.input.MigrateIndicatorConfig()

			// Verify old fields are cleared
			assert.Nil(t, tt.input.EMAPeriods, "Old EMA periods should be nil after migration")
			assert.Nil(t, tt.input.RSIPeriods, "Old RSI periods should be nil after migration")
			assert.Nil(t, tt.input.ATRPeriods, "Old ATR periods should be nil after migration")
			assert.Nil(t, tt.input.BOLLPeriods, "Old BOLL periods should be nil after migration")
			assert.Equal(t, 0.0, tt.input.BOXRatio, "Old BOX ratio should be 0 after migration")

			// Verify new fields are set correctly
			if tt.expected.EMA != nil {
				assert.NotNil(t, tt.input.EMA)
				assert.Equal(t, tt.expected.EMA.Periods, tt.input.EMA.Periods)
			}
			if tt.expected.RSI != nil {
				assert.NotNil(t, tt.input.RSI)
				assert.Equal(t, tt.expected.RSI.Periods, tt.input.RSI.Periods)
			}
			if tt.expected.ATR != nil {
				assert.NotNil(t, tt.input.ATR)
				assert.Equal(t, tt.expected.ATR.Periods, tt.input.ATR.Periods)
			}
			if tt.expected.BOLL != nil {
				assert.NotNil(t, tt.input.BOLL)
				assert.Equal(t, tt.expected.BOLL.Periods, tt.input.BOLL.Periods)
				assert.Equal(t, tt.expected.BOLL.StdDevMultiplier, tt.input.BOLL.StdDevMultiplier)
			}
			if tt.expected.BOX != nil {
				assert.NotNil(t, tt.input.BOX)
				assert.Equal(t, tt.expected.BOX.Ratio, tt.input.BOX.Ratio)
			}
		})
	}
}

func TestIndicatorConfigGetters(t *testing.T) {
	config := &IndicatorConfig{
		EMA:  &types.EMAParamConfig{Periods: []int{9, 20}},
		RSI:  &types.RSIParamConfig{Periods: []int{7, 14}},
		ATR:  &types.ATRParamConfig{Periods: []int{14}},
		BOLL: &types.BOLLParamConfig{Periods: []int{20}, StdDevMultiplier: 2.5},
		BOX:  &types.BOXParamConfig{Ratio: 1.05},
		MACD: &types.MACDParamConfig{FastPeriod: 10, SlowPeriod: 24, SignalPeriod: 8},
	}

	t.Run("GetEMAParamConfig", func(t *testing.T) {
		result := config.GetEMAParamConfig()
		assert.Equal(t, []int{9, 20}, result.Periods)
	})

	t.Run("GetRSIParamConfig", func(t *testing.T) {
		result := config.GetRSIParamConfig()
		assert.Equal(t, []int{7, 14}, result.Periods)
	})

	t.Run("GetATRParamConfig", func(t *testing.T) {
		result := config.GetATRParamConfig()
		assert.Equal(t, []int{14}, result.Periods)
	})

	t.Run("GetBOLLParamConfig", func(t *testing.T) {
		result := config.GetBOLLParamConfig()
		assert.Equal(t, []int{20}, result.Periods)
		assert.Equal(t, 2.5, result.StdDevMultiplier)
	})

	t.Run("GetBOXParamConfig", func(t *testing.T) {
		result := config.GetBOXParamConfig()
		assert.Equal(t, 1.05, result.Ratio)
	})

	t.Run("GetMACDPeriods", func(t *testing.T) {
		fast, slow, signal := config.GetMACDPeriods()
		assert.Equal(t, 10, fast)
		assert.Equal(t, 24, slow)
		assert.Equal(t, 8, signal)
	})
}

func TestIndicatorConfigDefaults(t *testing.T) {
	config := &IndicatorConfig{}

	t.Run("GetEMAParamConfig with empty config", func(t *testing.T) {
		result := config.GetEMAParamConfig()
		assert.Equal(t, []int{20, 50}, result.Periods)
	})

	t.Run("GetRSIParamConfig with empty config", func(t *testing.T) {
		result := config.GetRSIParamConfig()
		assert.Equal(t, []int{7, 14}, result.Periods)
	})

	t.Run("GetATRParamConfig with empty config", func(t *testing.T) {
		result := config.GetATRParamConfig()
		assert.Equal(t, []int{14}, result.Periods)
	})

	t.Run("GetBOLLParamConfig with empty config", func(t *testing.T) {
		result := config.GetBOLLParamConfig()
		assert.Equal(t, []int{20}, result.Periods)
		assert.Equal(t, 2.0, result.StdDevMultiplier)
	})

	t.Run("GetBOXParamConfig with empty config", func(t *testing.T) {
		result := config.GetBOXParamConfig()
		assert.Equal(t, 1.03, result.Ratio)
	})

	t.Run("GetMACDPeriods with empty config", func(t *testing.T) {
		fast, slow, signal := config.GetMACDPeriods()
		assert.Equal(t, 12, fast)
		assert.Equal(t, 26, slow)
		assert.Equal(t, 9, signal)
	})
}
