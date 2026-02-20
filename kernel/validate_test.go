package kernel

import (
	"testing"
)

// TestLeverageFallback tests automatic correction when leverage exceeds limit
func TestLeverageFallback(t *testing.T) {
	tests := []struct {
		name            string
		decision        Decision
		accountEquity   float64
		btcEthLeverage  int
		altcoinLeverage int
		wantLeverage    int // Expected leverage after correction
		wantError       bool
	}{
		{
			name: "Altcoin leverage exceeded - auto-correct to limit",
			decision: Decision{
				Symbol:          "SOLUSDT",
				Action:          "open_long",
				Leverage:        20, // Exceeds limit
				PositionSizeUSD: 100,
				StopLoss:        77,  // ~4% below current price
				TakeProfit:      88,  // ~10% above current price
			},
			accountEquity:   100,
			btcEthLeverage:  10,
			altcoinLeverage: 5, // Limit 5x
			wantLeverage:    5, // Should be corrected to 5
			wantError:       false,
		},
		{
			name: "BTC leverage exceeded - auto-correct to limit",
			decision: Decision{
				Symbol:          "BTCUSDT",
				Action:          "open_long",
				Leverage:        20, // Exceeds limit
				PositionSizeUSD: 1000,
				StopLoss:        64000,  // ~4% below current price
				TakeProfit:      70000,  // ~5% above current price
			},
			accountEquity:   100,
			btcEthLeverage:  10, // Limit 10x
			altcoinLeverage: 5,
			wantLeverage:    10, // Should be corrected to 10
			wantError:       false,
		},
		{
			name: "Leverage within limit - no correction",
			decision: Decision{
				Symbol:          "ETHUSDT",
				Action:          "open_short",
				Leverage:        5, // Not exceeded
				PositionSizeUSD: 500,
				StopLoss:        2050,  // ~6.5% above current price
				TakeProfit:      1750,  // ~9% below current price
			},
			accountEquity:   100,
			btcEthLeverage:  10,
			altcoinLeverage: 5,
			wantLeverage:    5, // Stays unchanged
			wantError:       false,
		},
		{
			name: "Leverage is 0 - should error",
			decision: Decision{
				Symbol:          "SOLUSDT",
				Action:          "open_long",
				Leverage:        0, // Invalid
				PositionSizeUSD: 100,
				StopLoss:        77,
				TakeProfit:      88,
			},
			accountEquity:   100,
			btcEthLeverage:  10,
			altcoinLeverage: 5,
			wantLeverage:    0,
			wantError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal context for testing
			ctx := &Context{
				Account: AccountInfo{
					TotalEquity: tt.accountEquity,
				},
			}

			// Use default position value ratios for testing (10x for BTC/ETH, 1.5x for altcoins)
			// Use minPositionSize of 12.0 USDT for testing
			// Use minConfidence of 0.0 (disabled) for testing
			err := validateDecision(&tt.decision, ctx, tt.btcEthLeverage, tt.altcoinLeverage, 10.0, 1.5, 12.0, 0.0, 1.5)

			// Check error status
			if (err != nil) != tt.wantError {
				t.Errorf("validateDecision() error = %v, wantError %v", err, tt.wantError)
				return
			}

			// If shouldn't error, check if leverage was correctly corrected
			if !tt.wantError && tt.decision.Leverage != tt.wantLeverage {
				t.Errorf("Leverage not corrected: got %d, want %d", tt.decision.Leverage, tt.wantLeverage)
			}
		})
	}
}


// contains checks if string contains substring (helper function)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestValidateStopLossTakeProfitPrices tests the price reasonableness validation
func TestValidateStopLossTakeProfitPrices(t *testing.T) {
	tests := []struct {
		name          string
		symbol        string
		action        string
		currentPrice  float64
		stopLoss      float64
		takeProfit    float64
		wantError     bool
		errorContains string
	}{
		// Valid cases - Long
		{
			name:         "Valid long - reasonable SL and TP",
			symbol:       "BTCUSDT",
			action:       "open_long",
			currentPrice: 100000,
			stopLoss:     97000,  // 3% stop loss
			takeProfit:   105000, // 5% take profit
			wantError:    false,
		},
		{
			name:         "Valid long - minimum SL",
			symbol:       "ETHUSDT",
			action:       "open_long",
			currentPrice: 3500,
			stopLoss:     3475, // ~0.7% stop loss
			takeProfit:   3700, // ~5.7% take profit
			wantError:    false,
		},
		{
			name:         "Valid long - maximum SL",
			symbol:       "SOLUSDT",
			action:       "open_long",
			currentPrice: 200,
			stopLoss:     165, // 17.5% stop loss
			takeProfit:   280, // 40% take profit
			wantError:    false,
		},

		// Valid cases - Short
		{
			name:         "Valid short - reasonable SL and TP",
			symbol:       "BTCUSDT",
			action:       "open_short",
			currentPrice: 100000,
			stopLoss:     103000, // 3% stop loss
			takeProfit:   95000,  // 5% take profit
			wantError:    false,
		},

		// Invalid cases - Long
		{
			name:          "Invalid long - SL above current price",
			symbol:        "BTCUSDT",
			action:        "open_long",
			currentPrice:  100000,
			stopLoss:      101000,
			takeProfit:    105000,
			wantError:     true,
			errorContains: "must be below current price",
		},
		{
			name:          "Invalid long - TP below current price",
			symbol:        "BTCUSDT",
			action:        "open_long",
			currentPrice:  100000,
			stopLoss:      97000,
			takeProfit:    99000,
			wantError:     true,
			errorContains: "must be above current price",
		},
		{
			name:          "Invalid long - SL too tight",
			symbol:        "BTCUSDT",
			action:        "open_long",
			currentPrice:  100000,
			stopLoss:      99900, // 0.1% stop loss - too tight
			takeProfit:    105000,
			wantError:     true,
			errorContains: "too tight for market volatility",
		},
		{
			name:          "Invalid long - SL too far",
			symbol:        "BTCUSDT",
			action:        "open_long",
			currentPrice:  100000,
			stopLoss:      70000, // 30% stop loss - too far
			takeProfit:    105000,
			wantError:     true,
			errorContains: "unreasonably large risk",
		},
		{
			name:          "Invalid long - TP too close",
			symbol:        "BTCUSDT",
			action:        "open_long",
			currentPrice:  100000,
			stopLoss:      97000,
			takeProfit:    100500, // 0.5% take profit - too close
			wantError:     true,
			errorContains: "not worth trading",
		},
		{
			name:          "Invalid long - TP too far",
			symbol:        "BTCUSDT",
			action:        "open_long",
			currentPrice:  100000,
			stopLoss:      97000,
			takeProfit:    200000, // 100% take profit - unrealistic
			wantError:     true,
			errorContains: "unrealistic target",
		},

		// Invalid cases - Short
		{
			name:          "Invalid short - SL below current price",
			symbol:        "BTCUSDT",
			action:        "open_short",
			currentPrice:  100000,
			stopLoss:      99000,
			takeProfit:    95000,
			wantError:     true,
			errorContains: "must be above current price",
		},
		{
			name:          "Invalid short - TP above current price",
			symbol:        "BTCUSDT",
			action:        "open_short",
			currentPrice:  100000,
			stopLoss:      103000,
			takeProfit:    101000,
			wantError:     true,
			errorContains: "must be below current price",
		},
		{
			name:          "Invalid short - SL too tight",
			symbol:        "BTCUSDT",
			action:        "open_short",
			currentPrice:  100000,
			stopLoss:      100200, // 0.2% stop loss - too tight
			takeProfit:    95000,
			wantError:     true,
			errorContains: "too tight for market volatility",
		},
		{
			name:          "Invalid short - TP too far",
			symbol:        "BTCUSDT",
			action:        "open_short",
			currentPrice:  100000,
			stopLoss:      103000,
			takeProfit:    30000, // 70% take profit - unrealistic
			wantError:     true,
			errorContains: "unrealistic target",
		},

		// Note: SL vs TP relationship is implicitly validated by the position checks
		// (SL vs current price and TP vs current price), so it's mathematically
		// impossible to have invalid SL vs TP while having valid positions relative to current price.
		// The SL vs TP check serves as a defensive validation.
		// These test cases would only fail if there's a bug in the validation logic itself.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStopLossTakeProfitPrices(tt.symbol, tt.action, tt.currentPrice, tt.stopLoss, tt.takeProfit)

			if (err != nil) != tt.wantError {
				t.Errorf("ValidateStopLossTakeProfitPrices() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError && tt.errorContains != "" {
				if !contains(err.Error(), tt.errorContains) {
					t.Errorf("Error message doesn't contain expected text.\nGot: %s\nExpected: %s", err.Error(), tt.errorContains)
				}
			}
		})
	}
}
