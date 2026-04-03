# Trading Issues & Strategy Deficiencies: Comprehensive Report

**Generated from analysis of 4 ETHUSDT positions (2026-03-07 to 03-09) and source code review.**

---

## Part 1: Critical Systemic Issues (Must Fix)

### 1.1 No Flash Crash / Volatility Cool-Down Detection

**Issue:** The system opened LONG positions immediately after major price collapses without any mandatory waiting period or volatility filter.

**Evidence:**
- **03-08 LONG**: Opened at 04:11 UTC, only **1.5 hours** after a -1.48% drop to $1934 with 8x normal volume. Triggered stop-loss in 43 minutes (-0.77%).
- **03-09 LONG**: Opened at 01:34 UTC, only **3 hours** after a flash crash to $1906 (-2.4% in 15 min, 10x volume). Closed at -0.82% after 59 minutes.

**Root Cause in Code:**
- In `kernel/engine.go` prompt (`BuildSystemPrompt`), there is NO mention of post-crash or high-volatility entry restrictions.
- The entry rules only state: `"Sideways or low-volatility markets are forbidden"` but do NOT say `"High-volatility post-crash markets are forbidden"`.
- `trader/auto_trader.go` has no backend logic to detect异常 volume spikes or sharp cascades and enforce a cooldown.

**Fix Required:**
```python
# pseudocode
IF (15m price_drop > 1.5% AND volume > 5x_average):
    mark "crash_event"
    cooldown = 4-6 hours
    reject_new_entries OR confidence_penalty = -20%
```

---

### 1.2 Confidence Level Ignores Market Environment

**Issue:** The prompt uses a rigid confidence-to-position-size mapping that doesn't penalize dangerous environments (post-crash, counter-trend).

**Evidence:**
- **03-08 LONG**: 85% confidence after a crash → used maximum 29 USDT position. This led to the largest loss (-0.21 USDT).
- **03-09 SHORT**: 78% confidence, but had strong signals and worked. So high confidence *can* work, but it's not adjusted for context.

**Root Cause in Code:**
- `kernel/engine.go:1248-1251` enforces:
  ```
  - ≥85 → 80–100% of max limit
  - 70–84 → 50–80% of max limit
  - 60–69 → 30–50% of max limit
  ```
- There is no discount factor for:
  - Being within 6 hours of a detected crash
  - Trading against the 4h/12h/24h trend
  - Entering on the first bounce without confirmed support

**Fix Required:**
Add explicit confidence penalties in the prompt and backend validation:
```
- Post-crash (< 6h): -20%
- Counter-trend (4h/12h opposite): -10%
- Support not yet validated (single bounce): -10%
- Funding rate opposite to trade direction: -5%
```

---

### 1.3 Stop-Loss Validation Is Static, Not Dynamic

**Issue:** `ValidateStopLossTakeProfitPrices` in `kernel/engine.go:2292` uses fixed percentage bands (min 0.5%, max 20%) without considering realized volatility.

**Evidence:**
- **03-08 LONG**: Stop loss was only 0.78% below entry. In the post-crash environment, this was hit in 43 minutes.
- **03-09 SHORT**: Stop loss was 0.71% above entry. This worked because volatility was lower by then, but was still tight.

**Root Cause in Code:**
```go
// kernel/engine.go:2294-2297
const (
    maxStopLossPercent   = 20.0
    minStopLossPercent   = 0.5
    minTakeProfitPercent = 1.0
)
```
- The system calculates ATR (`EnableATR` exists) but does NOT feed it into SL/TP validation.
- `ValidateRiskRewardRatio` also ignores ATR.

**Fix Required:**
Use ATR-adjusted stops:
```go
normalMarket:    stop_distance = max(1.0%, 1.0 × ATR)
postCrashMarket: stop_distance = max(2.0%, 1.5 × ATR)
```

---

## Part 2: Major Signal Interpretation Issues

### 2.1 Over-Reliance on 1h Institutional Fund Flow (Lagging/Noise)

**Issue:** The AI repeatedly treats 1-hour institutional netflow as a primary entry signal, but this is a **cumulative lagging indicator**. It captures dip-buying that may already be complete.

**Evidence:**
- **03-08 LONG**: +10.83M 1h institutional inflow ("strongest signal"). The AI entered at $1949, but by then the dip-buyers at $1940 had already filled their orders. Result: -0.77%.
- **03-09 LONG**: +66.29M 1h inflow looked bullish, but reversed to -15M in the next hour.

**Root Cause in Code:**
- In `kernel/engine.go` (`BuildUserPrompt`), the prompt lists all timeframes (5m, 15m, 1h, 4h, 12h, 24h) but does **not require a minimum number of positive timeframes**.
- The AI can cherry-pick the single most bullish 1h number while ignoring bearish 4h/12h/24h.

**Fix Required:**
Add a hard constraint in the prompt:
```
Entry Requirement (Fund Flow):
- For LONG: At least 2 of [15m, 1h, 4h] must show positive institutional flow.
- For SHORT: At least 2 of [15m, 1h, 4h] must show negative institutional flow.
- NEVER open based on a single timeframe's flow.
```

---

### 2.2 No Support/Resistance Validation Rules

**Issue:** The AI enters on "support bounce" after a single candle or short-lived recovery. There is no backend requirement that support must be *validated* (e.g., held for >2 hours or retested multiple times).

**Evidence:**
- **03-08 LONG**: Entered because price "rebounded from $1941". But this was just a dead-cat bounce after the crash.
- **03-09 LONG**: Entered because price "stabilized above $1936 support". The flash crash had just happened 3 hours earlier.

**Root Cause in Code:**
- The `EnableBOX` indicator provides support levels, but there is no validation engine checking:
  - How long price has held above support
  - Number of successful tests
  - Whether volume has normalized

**Fix Required:**
Add support validation logic:
```
A "valid support bounce" requires at least ONE of:
1. Price stays above support for ≥ 2 hours
2. At least 2 tests of support without breaking
3. Volume returns to normal (< 2x average)
4. Price makes a higher high after the bounce
```

---

### 2.3 Failure to Resolve Contradictory Signals

**Issue:** The prompt says "Contradictory signals are forbidden" but gives no instruction on **how to resolve contradictions** or **which signal takes priority**.

**Evidence:**
- **03-08 LONG**: Bullish 1h fund flow (+10.83M) vs bearish 4h/12h/24h trend + immediate post-crash volatility. AI chose the bullish flow and lost.
- **03-09 SHORT**: Bullish 1h fund flow (+152M) vs bearish technical rejection at resistance + 99.9% volume collapse. AI chose technicals and won — but this was an inconsistency.

**Root Cause in Code:**
- `kernel/engine.go:1257` states: `"Contradictory signals are forbidden"`.
- This is impossible to enforce perfectly by an LLM without a hierarchy of signal priority.

**Fix Required:**
Replace the vague rule with a priority ranking:
```
Signal Priority Hierarchy (short-term trades < 1h):
1. Volume anomaly (collapse/spike) - most immediate
2. Support/Resistance rejection - immediate
3. Multi-timeframe price action - medium
4. OI changes - medium-lag
5. Institutional fund flow (1h) - lagging, confirmatory only

When signals conflict, higher-priority signals override lower-priority ones.
```

---

## Part 3: Execution & Risk Management Deficiencies

### 3.1 No Automated Time-Based or Progress-Based Exits

**Issue:** The AI manually closed the 03-07 LONG after 1h14m for "no progress", but this logic is purely inside the LLM. There is no backend rule to enforce time stops or progress stops.

**Evidence:**
- **03-07 LONG**: Closed at +0.22% because "no significant progress after 1h14m" — a good discretionary call.
- **03-08 LONG**: If a similar rule had existed, the position could have been closed before hitting the stop-loss.
- **03-09 LONG**: Same — could have saved ~0.5% if there was a time-stop.

**Root Cause in Code:**
- `trader/auto_trader.go` has `peakPnLCache` but no logic to auto-close on:
  - Time elapsed without X% profit
  - Drawdown from peak exceeding a threshold
- `PositionInfo` has `PeakPnLPct` but it's sent to the AI for judgment, not used for automated exits.

**Fix Required:**
Add automated exit rules in `auto_trader.go`:
```go
// Time stop
if holdDuration > 60min and currentPnL < 0.5% {
    close_position("time_stop")
}

// Trailing drawdown stop
if peakPnL > 1.0% and currentPnL < peakPnL - 0.5% {
    close_position("trailing_stop")
}
```

---

### 3.2 Risk/Reward Validation Uses Fresh Market Price, Not Actual Entry

**Issue:** `ValidateRiskRewardRatio` in `kernel/engine.go:2367` validates R/R using `market.Get()` current price. By the time the order executes, the price may have moved, making the validated R/R irrelevant.

**Root Cause in Code:**
```go
if freshData, err := market.Get(d.Symbol); err == nil && freshData.CurrentPrice > 0 {
    actualEntryPrice = freshData.CurrentPrice
}
if err := ValidateRiskRewardRatio(..., actualEntryPrice, ...); err != nil {
    return err
}
```
- This validates R/R at decision-parse time, not execution time.
- On fast-moving markets (post-crash), the execution price can differ significantly.

**Fix Required:**
Re-validate R/R at order execution time in `auto_trader.go`. If the executed entry price no longer meets the minimum R/R, cancel or reduce the order.

---

### 3.3 "Immediate Re-entry" Rule Is Vague and Unenforced

**Issue:** The prompt says `"Immediate re-entry after exit is forbidden"`, but there's no code enforcement or clear definition of "immediate."

**Evidence:**
- **03-09 LONG**: Opened at 01:34 UTC, 1 hour after a previous short position closed (not the exact same direction, but still rapid re-engagement).
- More importantly, no backend check prevents re-entry within N minutes of a stop-loss hit.

**Root Cause in Code:**
- The rule exists only in the prompt (`kernel/engine.go:1259`).
- No backend logic in `auto_trader.go` tracks the last exit time per symbol and enforces a cooldown.

**Fix Required:**
Add cooldown tracking in `AutoTrader`:
```go
type SymbolCooldown struct {
    LastExitTime time.Time
    CooldownDuration time.Duration
}

// After SL hit: 2h cooldown for same symbol+direction
// After TP hit: 1h cooldown
```

---

## Part 4: Prompt Engineering & AI Behavior Issues

### 4.1 OI Interpretation Lacks Nuance

**Issue:** The prompt provides 4 OI scenarios, but the AI fails to distinguish between "new longs opening" (sustainable) and "short covering" (temporary) in post-crash environments.

**Evidence:**
- **03-08 LONG**: OI +17.66M with price +0.21% was read as "bullish new longs". In reality, much of this was likely shorts covering or new shorts hedging the bounce.
- **03-09 SHORT**: OI -11.49M with price +2.13% was correctly read as short covering, enabling a good SHORT entry on the pullback.
- The inconsistency shows the AI's interpretation is not reliable without more context.

**Root Cause in Code:**
- The OI guidance in the prompt is too simplistic for volatile post-crash markets.

**Fix Required:**
Add a volatile-market addendum to the OI rules:
```
Special Rule for Post-Crash Markets:
- OI increase + price increase within 4h of a sharp drop (>1.5%) may indicate SHORT COVERING,
  not new longs. Treat as temporary.
- OI decrease + price increase in the same conditions is classic short covering.
  Wait for the short-squeeze to exhaust before entering.
```

---

### 4.2 AI Calculates R/R Inconsistently

**Issue:** The AI's internal R/R calculations in its CoT trace are sometimes inconsistent with the prices it proposes.

**Evidence:**
- **03-08 LONG**: AI claimed "risk about 0.6%, reward about 6.2%, R/R over 10:1". Actual stop was $1935, entry ~$1950 = 0.78% risk. The AI understated risk.
- **03-09 LONG**: AI claimed R/R "1:4.5". Actual $1950 → $1925 = 1.28% risk, $1950 → $2050 = 5.1% reward = 1:4.0. Close enough, but manual variation shows lack of standardization.

**Root Cause:**
- The prompt does not provide a formula for the AI to use for R/R. It just says "min risk-reward ratio: 1:3.0".
- The AI estimates mentally, leading to rounding errors / optimistic bias.

**Fix Required:**
Add an explicit formula in the prompt:
```
RISK% = |entry_price - stop_loss| / entry_price × leverage × 100
REWARD% = |take_profit - entry_price| / entry_price × leverage × 100
R/R = REWARD% / RISK%

You MUST calculate R/R explicitly and state it in your reasoning.
```

---

### 4.3 Missing Volume-As-Signal Weight

**Issue:** The most successful trade (03-09 SHORT) was driven by a 99.9% volume collapse, but the prompt doesn't explicitly teach the AI that extreme volume anomalies are high-confidence signals.

**Evidence:**
- **03-09 SHORT**: Volume dropped from 291K to 165. This correctly signaled buyer exhaustion.
- In the other three trades, volume was either ignored or misread.

**Root Cause:**
- `EnableVolume` includes volume data in the prompt, but there is no rule like:
  `"If volume collapses to <1% of recent average after a sharp move, this indicates momentum exhaustion."`

**Fix Required:**
Add volume-specific guidance:
```
Volume Signals:
- After a sharp price move (>1.5% in 15m), if the next candle's volume drops to <10% of the move's volume, momentum is likely exhausted.
- If it drops to <1%, exhaustion is extremely high (use as primary signal).
```

---

## Part 5: Minor Issues & Improvements

### 5.1 Recent Trade History Bias

**Issue:** The prompt always shows 10 recent trades. For ETHUSDT-only trading, this creates a strong recency bias because the AI sees its own recent losses.

**Root Cause in Code:**
- `auto_trader.go:907-935` fetches `GetRecentTrades(at.id, 10)` and includes them in `ctx.RecentOrders`.
- When the AI has just lost 2-3 trades, it may become overly cautious or overly aggressive to "make it back."

**Fix:**
Consider showing only symbol-specific stats rather than every recent trade narrative.

### 5.2 No Automated Feedback Loop from Losses

**Issue:** The system has a backtest engine (`backtest/runner.go`) and stats collection, but there is no automated rule adjustment when specific failure patterns repeat.

**Evidence:**
- Post-crash entries failed on **both 03-08 and 03-09**.
- The system did not learn or tighten rules after the first failure.

**Fix:**
Add a simple pattern tracker:
```
IF (last 3 entries after crashes all lost):
    increase_cooldown_from_4h_to_6h()
```

### 5.3 Grid and Strategy Modes May Co-Exist Unsafely

**Issue:** `AutoTrader` carries a `gridState` field. While not active in these trades, the coexistence of grid trading and AI discretionary trading in the same struct could lead to unexpected conflicts.

---

## Summary Table: Issues vs. Trades

| Issue | 03-07 LONG | 03-08 LONG | 03-09 LONG | 03-09 SHORT |
|-------|------------|------------|------------|-------------|
| **Post-crash entry too early** | N/A | ✅ Hit | ✅ Hit | N/A |
| **Static SL too tight** | N/A | ✅ Hit | N/A | N/A |
| **Over-reliance on 1h flow** | Minor | ✅ Major | ✅ Major | N/A |
| **No multi-timeframe flow requirement** | N/A | ✅ Hit | N/A | N/A |
| **No support validation** | N/A | ✅ Hit | ✅ Hit | N/A |
| **No time-stop rule** | AI did manually | Would have helped | Would have helped | N/A |
| **Confidence too high post-crash** | N/A | ✅ Hit | N/A | N/A |
| **Volume signal underweighted** | N/A | N/A | N/A | ✅ Success |
| **Contradiction resolution missing** | Minor | ✅ Hit | N/A | Minor |

---

## Recommended Priority Order for Fixes

### P0 (Critical - Prevent Major Losses)
1. **Add flash crash / volatility spike detection with mandatory cooldown**
2. **Dynamic stop-loss based on ATR or recent realized volatility**
3. **Confidence adjustment factors for market environment**

### P1 (High - Improve Win Rate)
4. **Multi-timeframe fund flow confirmation requirement**
5. **Support/Resistance validation rules (time + retests)**
6. **Automated time-stop / trailing drawdown exit**

### P2 (Medium - Polish)
7. **Volume anomaly scoring in prompt**
8. **Explicit R/R calculation formula in prompt**
9. **Re-entry cooldown enforcement in backend**
10. **Pattern-based adaptive rule tightening**

---

## Part 6: Code Investigation Results (2026-04-03)

### 6.1 Confirmed: Zero Flash Crash Detection Implementation

**Investigation Date:** 2026-04-03
**Files Reviewed:** `kernel/engine.go`, `trader/auto_trader.go`, `market/data.go`, `market/types.go`

#### Evidence from Code Review:

**market/types.go (lines 165-170):**
```go
type AlertThresholds struct {
    VolumeSpike      float64 `json:"volume_spike"`      // 3.0 default
    PriceChange15Min float64 `json:"price_change_15min"` // 0.05 default
    VolumeTrend      float64 `json:"volume_trend"`
    RSIOverbought    float64 `json:"rsi_overbought"`
    RSIOversold      float64 `json:"rsi_overbought"`
}
```
- **Status:** ❌ **Defined but NEVER used**
- **Search result:** No calculation logic found in `market/data.go`
- **Conclusion:** AlertThresholds struct exists but no actual volume spike or price crash detection is implemented

**kernel/engine.go (line 1258):**
```go
sb.WriteString("- Sideways or low-volatility markets are forbidden\n")
```
- **Status:** ❌ **One-sided restriction**
- **Missing:**
  - No mention of "High-volatility post-crash markets are forbidden"
  - No mention of "Abnormal volume spike entries are forbidden"
  - No instruction to check recent crash events before entry

**kernel/engine.go (lines 2293-2297):**
```go
const (
    maxStopLossPercent   = 20.0
    minStopLossPercent   = 0.5  // Fixed minimum
    minTakeProfitPercent = 1.0
    maxTakeProfitPercent = 50.0
)
```
- **Status:** ❌ **Static validation, ignores market conditions**
- **Problems:**
  - `minStopLossPercent = 0.5%` is enforced regardless of volatility
  - ATR is calculated (`calculateATR` exists in `market/data.go:746`) but NOT used in validation
  - No adjustment for post-crash environments where stops need wider buffer

**trader/auto_trader.go (line 123):**
```go
type AutoTrader struct {
    // ...
    peakPnLCache          map[string]float64 // Peak profit cache
    // ...
}
```
- **Status:** ❌ **No crash tracking at all**
- **Missing fields:**
  - No `lastExitTime` tracking per symbol
  - No `crashEvents` cache
  - No `cooldownUntil` map
- **Unused potential:** `peakPnLCache` exists but has no automated exit logic

**trader/auto_trader.go (grep results):**
```bash
$ grep -n "crashDetect\|lastExit\|cooldown" trader/auto_trader.go
# (No results - these concepts don't exist)
```

#### Verification of Existing Analysis Claims:

**Claim from Part 1.1:** "The system opened LONG positions immediately after major price collapses without any mandatory waiting period"
- ✅ **CONFIRMED** - No crash detection code exists

**Claim from Part 1.1:** "In `kernel/engine.go` prompt, there is NO mention of post-crash or high-volatility entry restrictions"
- ✅ **CONFIRMED** - Line 1258 only mentions low-volatility restriction

**Claim from Part 1.1:** "`trader/auto_trader.go` has no backend logic to detect abnormal volume spikes or sharp cascades"
- ✅ **CONFIRMED** - No relevant functions found in entire codebase

**Claim from Part 1.3:** "`ValidateStopLossTakeProfitPrices` uses fixed percentage bands without considering realized volatility"
- ✅ **CONFIRMED** - Lines 2293-2297 use hardcoded constants, ATR is not referenced

**Claim from Part 3.1:** "`peakPnLCache` exists but no logic to auto-close on time stops or drawdown stops"
- ✅ **CONFIRMED** - Cache defined at line 123, but only used for logging (lines 1854-1876), no automated action triggers

---

### 6.2 Specific Code Locations Summary

| Issue | File | Line(s) | Status |
|-------|------|---------|--------|
| No crash detection function | trader/auto_trader.go | N/A | ❌ Not implemented |
| No volume spike scanner | market/data.go | N/A | ❌ Not implemented |
| AlertThresholds unused | market/types.go | 165-170 | ❌ Defined but dead code |
| One-sided volatility rule | kernel/engine.go | 1258 | ❌ Only forbids low-vol |
| Static stop loss validation | kernel/engine.go | 2293-2297 | ❌ Ignores ATR |
| Peak P&L cache unused | trader/auto_trader.go | 123, 1854-1876 | ⚠️ Tracked but no action |
| No cooldown tracking | trader/auto_trader.go | N/A | ❌ Not implemented |
| No last exit time map | trader/auto_trader.go | N/A | ❌ Not implemented |

---

### 6.3 Critical Gap: Volatility Asymmetry

**The Prompt Forbids Low Volatility But Allows Extreme Volatility:**

From `kernel/engine.go:1258`:
```
- Sideways or low-volatility markets are forbidden
```

**This creates a dangerous asymmetry:**
- ✅ AI avoids boring, safe markets
- ❌ AI active in explosive, dangerous markets (flash crashes)
- ❌ No restriction on entering during volatility spikes
- ❌ No dynamic risk adjustment for extreme conditions

**What should exist:**
```go
// Example missing prompt text:
sb.WriteString("- Post-crash markets (< 6h after >1.5% drop with >5x volume) are forbidden\n")
sb.WriteString("- Volatility spike markets (ATR > 2x average) require wider stops\n")
```

---

### 6.4 ATR Calculation Exists But Not Used

**Found in market/data.go:746-777:**
```go
func calculateATR(klines []Kline, period int) float64 {
    // ... full ATR calculation implementation exists
}
```

**Usage:**
- ✅ ATR is calculated and stored in `TimeframeSeriesData.ATR14`
- ✅ ATR is displayed in market data output
- ❌ ATR is NEVER used in:
  - Stop loss validation (`ValidateStopLossTakeProfitPrices`)
  - Risk/reward calculation (`ValidateRiskRewardRatio`)
  - Entry condition checks
  - Confidence adjustment

**This is ready-to-use infrastructure that's completely ignored in risk management.**

---

### 6.5 Root Cause Diagnosis

**Primary Issue:** The system has a **reactive architecture** with no **proactive safety barriers**

**What exists:**
- ✅ Decision validation (checks after AI decides)
- ✅ Risk/reward ratio validation (mathematical sanity check)
- ✅ Stop loss distance validation (basic range check)

**What's missing:**
- ❌ Market state detection (is this a crash environment?)
- ❌ Environmental risk adjustment (should confidence be lowered?)
- ❌ Automated protective exits (time stops, trailing stops)
- ❌ Cooldown enforcement (hard barriers, not AI discretion)

**Analogy:** The system has excellent brakes (stop losses, validation) but no collision detection system (crash detection, cooldowns). It will happily accelerate into a wall and then brake perfectly at the last moment - but sometimes too late.

---

### 6.6 Actionable Implementation Roadmap

**Phase 1: Critical Safety (Implement Now)**

1. **Add crash detection in trader/auto_trader.go:**
```go
type CrashEvent struct {
    Symbol      string
    Timestamp   time.Time
    PriceDrop   float64  // Percentage
    VolumeRatio float64  // vs normal
}

func (at *AutoTrader) detectCrash(symbol string, klines []market.Kline) *CrashEvent {
    if len(klines) < 2 {
        return nil
    }

    latest := klines[len(klines)-1]
    prev := klines[len(klines)-2]

    priceDrop := (prev.Close - latest.Low) / prev.Close
    avgVolume := calculateAvgVolume(klines[:len(klines)-1])
    volumeRatio := latest.Volume / avgVolume

    if priceDrop > 0.015 && volumeRatio > 5.0 {
        return &CrashEvent{
            Symbol:      symbol,
            Timestamp:   time.UnixMilli(latest.OpenTime),
            PriceDrop:   priceDrop * 100,
            VolumeRatio: volumeRatio,
        }
    }
    return nil
}
```

2. **Add cooldown enforcement in trader/auto_trader.go:**
```go
type AutoTrader struct {
    // ... existing fields
    crashCooldowns map[string]time.Time // symbol -> cooldown expiry
}

func (at *AutoTrader) isCooldownActive(symbol string) bool {
    expiry, exists := at.crashCooldowns[symbol]
    if !exists {
        return false
    }
    return time.Now().Before(expiry)
}

func (at *AutoTrader) setCooldown(symbol string, duration time.Duration) {
    at.crashCooldowns[symbol] = time.Now().Add(duration)
}
```

3. **Update prompt in kernel/engine.go (line ~1258):**
```go
sb.WriteString("- Sideways or low-volatility markets are forbidden\n")
sb.WriteString("- High-volatility post-crash markets (< 6h after >1.5% drop) are forbidden\n")
sb.WriteString("- Volume spike entries (> 5x normal volume) require caution\n")
```

**Phase 2: Dynamic Risk Management (Next Sprint)**

4. **ATR-adjusted stop loss in kernel/engine.go:**
```go
func ValidateStopLossTakeProfitPrices(symbol, action string, currentPrice, stopLoss, takeProfit, atr float64) error {
    // Dynamic minimum based on ATR
    minStopLossPercent := math.Max(0.5, (atr / currentPrice) * 100)

    // If recent crash detected, double the minimum
    // (requires passing crash context)

    // ... rest of validation
}
```

5. **Confidence adjustment in prompt:**
```go
sb.WriteString("## Confidence Adjustment Factors\n")
sb.WriteString("Base confidence = signal strength (fund flow + OI + price action)\n\n")
sb.WriteString("Adjustments:\n")
sb.WriteString("- Post-crash (< 6h): -20%\n")
sb.WriteString("- Counter-trend (4h/12h opposite): -10%\n")
sb.WriteString("- Negative funding rate: -5%\n")
sb.WriteString("- Stop loss < 1% in high volatility: -10%\n\n")
sb.WriteString("Final confidence must still be ≥ minimum_threshold to proceed.\n")
```

**Phase 3: Automated Exits (Enhancement)**

6. **Time-based exits in trader/auto_trader.go:**
```go
func (at *AutoTrader) checkTimeStop(position *PositionInfo) *Decision {
    holdDuration := time.Since(time.UnixMilli(position.UpdateTime))

    if holdDuration > 60*time.Minute && position.UnrealizedPnLPct < 0.5 {
        return &Decision{
            Action: "close",
            Reason: "time_stop_no_progress",
        }
    }
    return nil
}
```

7. **Trailing drawdown stop in trader/auto_trader.go:**
```go
func (at *AutoTrader) checkTrailingStop(position *PositionInfo) *Decision {
    peakPnL := at.peakPnLCache[position.Symbol+"_"+position.Side]

    if peakPnL > 1.0 && position.UnrealizedPnLPct < peakPnL-0.5 {
        return &Decision{
            Action: "close",
            Reason: "trailing_stop_profit_protection",
        }
    }
    return nil
}
```

---

### 6.7 Validation: What These Changes Would Have Prevented

**03-08 LONG Trade (Lost -0.77%):**
1. Crash detection: ⚠️ Detected 02:45 crash (-1.48%, 8x volume)
2. Cooldown check: ❌ Entry at 04:11 (1.5h later) → BLOCKED
3. Confidence: Would drop from 85% → 55% (below threshold)
4. **Result:** Trade would NOT have opened ✅

**03-09 LONG Trade (Lost -0.82%):**
1. Crash detection: ⚠️ Detected flash crash (-2.4%, 10x volume)
2. Cooldown check: ❌ Entry at 01:34 (3h later) → BLOCKED
3. Confidence: Would drop from 78% → 58% (below threshold)
4. **Result:** Trade would NOT have opened ✅

**03-09 SHORT Trade (Won +0.36%):**
1. Crash detection: ✅ Entry at 05:26 (many hours after crash)
2. Cooldown check: ✅ No active cooldown
3. Confidence: No penalty applied
4. **Result:** Trade opens normally ✅

---

## Summary: Systemic vs Discretionary Failures

**03-07 Trade (Discretionary Issue):**
- Time-stop decision was AI judgment call
- Exit may have been too conservative
- **Type:** Strategy refinement needed
- **Severity:** Low (trade was profitable)

**03-08 & 03-09 LONG Trades (Systemic Failures):**
- No code to detect crashes
- No code to enforce cooldowns
- No code to adjust confidence for environment
- **Type:** Missing safety systems
- **Severity:** Critical (repeated losses from identical pattern)

**Conclusion:** The March 7th trade analysis (should you have held longer?) is debatable. The March 8th/9th trade failures are NOT debatable - they are **code-level defects** that must be fixed before any trading continues.

---

*This report combines specific trade forensics with source-code analysis of `trader/auto_trader.go`, `kernel/engine.go`, and `manager/trader_manager.go`.*

**Code Investigation Added: 2026-04-03**
