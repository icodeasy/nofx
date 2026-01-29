# Order & Position Lifecycle Documentation

This document provides a **deep dive** into the complete lifecycle of orders and positions in the NOFX trading system, from creation to closure, with **code evidence** for each stage.

---

## Table of Contents

1. [Order Lifecycle](#order-lifecycle)
   - [1.1 Order Creation](#11-order-creation)
   - [1.2 Order Submission](#12-order-submission)
   - [1.3 Order Recording](#13-order-recording)
   - [1.4 Order Status Polling](#14-order-status-polling)
   - [1.5 Order Fill](#15-order-fill)
   - [1.6 Order Sync (Background)](#16-order-sync-background)
2. [Position Lifecycle](#position-lifecycle)
   - [2.1 Position Opening](#21-position-opening)
   - [2.2 Position Monitoring](#22-position-monitoring)
   - [2.3 Position Closure](#23-position-closure)
   - [2.4 Position History](#24-position-history)
3. [Data Models](#data-models)
4. [Key Files Reference](#key-files-reference)

---

## Order Lifecycle

### 1.1 Order Creation

**Trigger:** AI decides to open or close a position

**Location:** `trader/auto_trader.go:488-665`

```go
func (at *AutoTrader) runCycle() error {
    // ... build context, call AI ...

    // AI makes decision
    aiDecision, err := kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, "balanced")

    // Process decision
    for _, decision := range aiDecision.Decisions {
        switch decision.Action {
        case "open_long":
            at.executeOpenLongWithRecord(&decision, &actionRecord)
        case "open_short":
            at.executeOpenShortWithRecord(&decision, &actionRecord)
        case "close_long":
            at.executeCloseLongWithRecord(&decision, &actionRecord)
        case "close_short":
            at.executeCloseShortWithRecord(&decision, &actionRecord)
        }
    }
}
```

**What happens:**
1. Trading cycle triggers (default every 3 minutes)
2. Market data fetched and context built
3. AI analyzes and outputs decision
4. Decision action determines order type

---

### 1.2 Order Submission

**Trigger:** Execution function called based on decision action

**Location:** `trader/auto_trader.go:1117-1144`

```go
func (at *AutoTrader) executeOpenLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
    logger.Infof("  📈 Open long: %s", decision.Symbol)

    // Get current price
    marketData, err := market.Get(decision.Symbol)
    if err != nil {
        return err
    }

    // ... risk checks ...

    // Open position
    order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
    if err != nil {
        return err
    }

    // Record order ID
    if orderID, ok := order["orderId"].(int64); ok {
        actionRecord.OrderID = orderID
    }

    logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

    return nil
}
```

**What happens:**
1. Get current market price
2. Calculate quantity based on position size
3. Apply risk controls (leverage limits, position size, margin)
4. Call exchange-specific `OpenLong()` method
5. Exchange returns order confirmation with `orderId`

**Evidence - Exchange Implementation (Binance):**

**Location:** `trader/binance_futures.go:447-485`

```go
func (t *FuturesTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
    symbol = market.Normalize(symbol)

    // Set leverage
    err := t.SetLeverage(symbol, leverage)
    if err != nil {
        return nil, err
    }

    // Create market order
    order, err := t.client.NewCreateOrderService().
        Symbol(symbol).
        Side("BUY").
        Type("MARKET").
        Quantity(fmt.Sprintf("%.6f", quantity)).
        PositionSide("LONG").
        Do(context.Background())

    if err != nil {
        return nil, err
    }

    // Return order info
    return map[string]interface{}{
        "orderId": order.OrderID,
        "symbol":  symbol,
        "clientOrderId": order.ClientOrderID,
    }, nil
}
```

---

### 1.3 Order Recording

**Trigger:** Immediately after order submission

**Location:** `trader/auto_trader.go:1130` (called after `OpenLong`)

```go
// Record order to database and poll for confirmation
at.recordAndConfirmOrder(order, decision.Symbol, "open_long", quantity, marketData.CurrentPrice, decision.Leverage, 0)
```

**Two paths based on exchange:**

#### Path A: Exchanges WITH OrderSync (Binance, Bybit, OKX, etc.)

**Location:** `trader/auto_trader.go:1926-1932`

```go
// Exchanges with OrderSync: Skip immediate order recording, let OrderSync handle it
// This ensures accurate data from GetTrades API and avoids duplicate records
switch at.exchange {
case "binance", "lighter", "hyperliquid", "bybit", "okx", "bitget", "aster":
    logger.Infof("  📝 Order submitted (id: %s), will be synced by OrderSync", orderID)
    return  // ⚠️ Early return - no immediate recording
}
```

**Why?** These exchanges have reliable trade history APIs. The OrderSync mechanism will fetch accurate fill data from exchange.

#### Path B: Exchanges WITHOUT OrderSync (fallback)

**Location:** `trader/auto_trader.go:1934-1940`

```go
// For exchanges without OrderSync: record immediately and poll for fill data
orderRecord := at.createOrderRecord(orderID, symbol, action, positionSide, quantity, price, leverage)
if err := at.store.Order().CreateOrder(orderRecord); err != nil {
    logger.Infof("  ⚠️ Failed to record order: %v", err)
} else {
    logger.Infof("  📝 Order recorded: %s [%s] %s", orderID, action, symbol)
}
```

**Create Order Record:**

**Location:** `trader/auto_trader.go:2056-2103`

```go
func (at *AutoTrader) createOrderRecord(orderID, symbol, action, positionSide string, quantity, price float64, leverage int) *store.TraderOrder {
    return &store.TraderOrder{
        TraderID:        at.id,
        ExchangeID:      at.exchangeID,
        ExchangeType:    at.exchange,
        ExchangeOrderID: orderID,
        Symbol:          normalizedSymbol,
        Side:            side,              // BUY or SELL
        PositionSide:    positionSide,      // LONG or SHORT
        Type:            "MARKET",
        TimeInForce:     "GTC",
        Quantity:        quantity,
        Price:           price,
        Status:          "NEW",             // ⚠️ Initial status
        FilledQuantity:  0,
        AvgFillPrice:    0,
        Commission:      0,
        Leverage:        leverage,
        ReduceOnly:      reduceOnly,
        ClosePosition:   reduceOnly,
        OrderAction:     action,            // open_long, close_short, etc.
        CreatedAt:       time.Now().UTC().UnixMilli(),
        UpdatedAt:       time.Now().UTC().UnixMilli(),
    }
}
```

**Database Table:**

**Location:** `store/order.go:13-43`

```go
type TraderOrder struct {
    ID                int64   `gorm:"primaryKey;autoIncrement"`
    TraderID          string  `gorm:"column:trader_id;not null;index"`
    ExchangeID        string  `gorm:"column:exchange_id;not null"`
    ExchangeType      string  `gorm:"column:exchange_type;not null"`
    ExchangeOrderID   string  `gorm:"column:exchange_order_id;not null;uniqueIndex"`
    Symbol            string  `gorm:"column:symbol;not null"`
    Side              string  `gorm:"column:side;not null"`           // BUY/SELL
    PositionSide      string  `gorm:"column:position_side"`           // LONG/SHORT
    Type              string  `gorm:"column:type;not null"`           // MARKET/LIMIT
    Quantity          float64 `gorm:"column:quantity;not null"`
    Price             float64 `gorm:"column:price"`
    Status            string  `gorm:"column:status;not null;default:NEW"`  // ⚠️ KEY FIELD
    FilledQuantity    float64 `gorm:"column:filled_quantity;default:0"`
    AvgFillPrice      float64 `gorm:"column:avg_fill_price;default:0"`
    Commission        float64 `gorm:"column:commission;default:0"`
    Leverage          int     `gorm:"column:leverage;default:1"`
    ReduceOnly        bool    `gorm:"column:reduce_only;default:false"`
    OrderAction       string  `gorm:"column:order_action"`            // open_long, close_short, etc.
    CreatedAt         int64   `gorm:"column:created_at"`             // Unix ms UTC
    UpdatedAt         int64   `gorm:"column:updated_at"`             // Unix ms UTC
    FilledAt          int64   `gorm:"column:filled_at"`               // Unix ms UTC
}
```

---

### 1.4 Order Status Polling

**Trigger:** Only for exchanges WITHOUT OrderSync

**Location:** `trader/auto_trader.go:1942-1982`

```go
// Wait for order to be filled and get actual fill data
time.Sleep(500 * time.Millisecond)  // Initial delay

for i := 0; i < 5; i++ {  // Max 5 retries (2.5 seconds total)
    status, err := at.trader.GetOrderStatus(symbol, orderID)
    if err == nil {
        statusStr, _ := status["status"].(string)

        if statusStr == "FILLED" {
            // Get actual fill data
            if avgPrice, ok := status["avgPrice"].(float64); ok && avgPrice > 0 {
                actualPrice = avgPrice
            }
            if execQty, ok := status["executedQty"].(float64); ok && execQty > 0 {
                actualQty = execQty
            }
            if commission, ok := status["commission"].(float64); ok {
                fee = commission
            }

            logger.Infof("  ✅ Order filled: avgPrice=%.6f, qty=%.6f, fee=%.6f", actualPrice, actualQty, fee)

            // Update order status to FILLED
            at.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", actualQty, actualPrice, fee)

            break
        } else if statusStr == "CANCELED" || statusStr == "EXPIRED" || statusStr == "REJECTED" {
            logger.Infof("  ⚠️ Order %s, skipping position record", statusStr)
            at.store.Order().UpdateOrderStatus(orderRecord.ID, statusStr, 0, 0, 0)
            return
        }
    }
    time.Sleep(500 * time.Millisecond)  // Wait before next poll
}
```

**What happens:**
1. Wait 500ms for exchange to process
2. Poll `GetOrderStatus()` every 500ms
3. Max 5 attempts (2.5 seconds total)
4. On FILLED: extract actual fill price/quantity/fee
5. On FAILED/CANCELED: record status and exit

**Interface Definition:**

**Location:** `trader/interface.go:92-94`

```go
type Trader interface {
    // GetOrderStatus Get order status
    GetOrderStatus(symbol string, orderID string) (map[string]interface{}, error)
}
```

**Implementation Example (Binance):**

**Location:** `trader/binance_futures.go:871-907`

```go
func (t *FuturesTrader) GetOrderStatus(symbol string, orderID string) (map[string]interface{}, error) {
    order, err := t.client.NewGetOrderService().
        Symbol(symbol).
        OrderID(orderID).
        Do(context.Background())

    if err != nil {
        return nil, err
    }

    return map[string]interface{}{
        "symbol":           order.Symbol,
        "orderId":          order.OrderID,
        "status":           order.Status,             // NEW, PARTIALLY_FILLED, FILLED, CANCELED, etc.
        "side":             order.Side,
        "type":             order.Type,
        "avgPrice":         parseFloat(order.AvgPrice),
        "executedQty":      parseFloat(order.ExecutedQty),
        "cumQty":           parseFloat(order.CumQty),
        "commission":       0.0,  // Filled later from user data stream
    }, nil
}
```

---

### 1.5 Order Fill

**Trigger:** Order status becomes "FILLED"

**Actions taken:**

#### A. Update Order Status

**Location:** `store/order.go:168-183`

```go
func (s *OrderStore) UpdateOrderStatus(id int64, status string, filledQty, avgPrice, commission float64) error {
    updates := map[string]interface{}{
        "status":          status,           // "FILLED"
        "filled_quantity": filledQty,
        "avg_fill_price":  avgPrice,
        "commission":      commission,
        "updated_at":      time.Now().UTC().UnixMilli(),
    }

    if status == "FILLED" {
        updates["filled_at"] = time.Now().UTC().UnixMilli()  // ⚠️ Record fill time
    }

    return s.db.Model(&TraderOrder{}).Where("id = ?", id).Updates(updates).Error
}
```

#### B. Record Fill Details

**Location:** `trader/auto_trader.go:2105-2149`

```go
func (at *AutoTrader) recordOrderFill(orderRecordID int64, exchangeOrderID, symbol, action string, price, quantity, fee float64) {
    // Generate trade ID
    tradeID := fmt.Sprintf("%s-%d", exchangeOrderID, time.Now().UnixNano())

    fill := &store.TraderFill{
        TraderID:         at.id,
        ExchangeID:       at.exchangeID,
        ExchangeType:     at.exchange,
        OrderID:          orderRecordID,
        ExchangeOrderID:  exchangeOrderID,
        ExchangeTradeID:  tradeID,
        Symbol:           normalizedSymbol,
        Side:             side,
        Price:            price,
        Quantity:         quantity,
        QuoteQuantity:    price * quantity,
        Commission:       fee,
        CommissionAsset:  "USDT",
        RealizedPnL:      0,  // Will be calculated on position close
        IsMaker:          false,  // Market orders are taker
        CreatedAt:        time.Now().UTC().UnixMilli(),
    }

    if err := at.store.Order().CreateFill(fill); err != nil {
        logger.Infof("  ⚠️ Failed to record fill: %v", err)
    }
}
```

**Database Table:**

**Location:** `store/order.go:52-75`

```go
type TraderFill struct {
    ID              int64   `gorm:"primaryKey;autoIncrement"`
    TraderID        string  `gorm:"column:trader_id;not null;index"`
    ExchangeID      string  `gorm:"column:exchange_id;not null"`
    ExchangeType    string  `gorm:"column:exchange_type;not null"`
    OrderID         int64   `gorm:"column:order_id;not null;index"`
    ExchangeOrderID string  `gorm:"column:exchange_order_id;not null"`
    ExchangeTradeID string  `gorm:"column:exchange_trade_id;not null;uniqueIndex"`
    Symbol          string  `gorm:"column:symbol;not null"`
    Side            string  `gorm:"column:side;not null"`
    Price           float64 `gorm:"column:price;not null"`
    Quantity        float64 `gorm:"column:quantity;not null"`
    QuoteQuantity   float64 `gorm:"column:quote_quantity;not null"`
    Commission      float64 `gorm:"column:commission;not null"`
    CommissionAsset string  `gorm:"column:commission_asset;not null"`
    RealizedPnL     float64 `gorm:"column:realized_pnl;default:0"`
    IsMaker         bool    `gorm:"column:is_maker;default:false"`
    CreatedAt       int64   `gorm:"column:created_at"`  // Unix ms UTC
}
```

#### C. Create or Update Position

**Location:** `trader/auto_trader.go:1991` (calls `recordPositionChange`)

```go
at.recordPositionChange(orderID, normalizedSymbolForPosition, positionSide, action, actualQty, actualPrice, leverage, entryPrice, fee)
```

**Position Recording Logic:**

**Location:** `trader/auto_trader.go:2006-2054`

```go
func (at *AutoTrader) recordPositionChange(orderID, symbol, side, action string, quantity, price float64, leverage int, entryPrice float64, fee float64) {
    switch action {
    case "open_long", "open_short":
        // Create new position record
        nowMs := time.Now().UTC().UnixMilli()
        pos := &store.TraderPosition{
            TraderID:     at.id,
            ExchangeID:   at.exchangeID,
            ExchangeType: at.exchange,
            Symbol:       symbol,
            Side:         side,
            Quantity:     quantity,
            EntryPrice:   price,
            EntryOrderID: orderID,
            EntryTime:    nowMs,
            Leverage:     leverage,
            Status:       "OPEN",  // ⚠️ Initial status
            Fee:          fee,
            CreatedAt:    nowMs,
            UpdatedAt:    nowMs,
        }
        at.store.Position().Create(pos)

    case "close_long", "close_short":
        // Close position using PositionBuilder
        posBuilder := store.NewPositionBuilder(at.store.Position())
        posBuilder.ProcessTrade(
            at.id, at.exchangeID, at.exchange,
            symbol, side, action,
            quantity, price, fee, 0,  // realizedPnL calculated by PositionBuilder
            time.Now().UTC().UnixMilli(), orderID,
        )
    }
}
```

---

### 1.6 Order Sync (Background)

**Trigger:** Periodic background sync for exchanges with OrderSync

**Purpose:** Fetch accurate trade history from exchange and reconstruct positions

**Supported Exchanges:** Binance, Bybit, OKX, Hyperliquid, Aster, Lighter, Bitget

**How it works:**

#### Step 1: Detect Symbols with Activity

**Location:** `trader/binance_order_sync.go:69-111`

```go
func (t *FuturesTrader) SyncOrdersFromBinance(traderID string, exchangeID string, exchangeType string, st *store.Store) error {
    // Method 1: COMMISSION income detection
    commissionSymbols, err := t.GetCommissionSymbols(lastSyncTime)

    // Method 2: Active positions
    positionSymbols := t.getPositionSymbols()

    // Method 3: Recent fills in DB
    recentSymbols, _ := orderStore.GetRecentFillSymbolsByExchange(exchangeID, lastSyncTimeMs)

    // Method 4: REALIZED_PNL income (catches closed positions)
    pnlSymbols, err := t.GetPnLSymbols(lastSyncTime)

    // Combine all methods
    for s := range commissionSymbols { symbolMap[s] = true }
    for s := range positionSymbols { symbolMap[s] = true }
    for s := range recentSymbols { symbolMap[s] = true }
    for s := range pnlSymbols { symbolMap[s] = true }
}
```

#### Step 2: Fetch Trades from Exchange

**Location:** `trader/binance_order_sync.go:129-159`

```go
for _, symbol := range changedSymbols {
    var trades []TradeRecord
    var queryErr error

    if lastID, ok := maxTradeIDs[symbol]; ok && lastID > 0 {
        // Incremental sync: query from last known trade ID
        trades, queryErr = t.GetTradesForSymbolFromID(symbol, lastID+1, 500)
    } else {
        // New symbol: query by time
        trades, queryErr = t.GetTradesForSymbol(symbol, lastSyncTime, 500)
    }

    allTrades = append(allTrades, trades...)
}
```

#### Step 3: Process Trades and Build Positions

**Location:** `trader/binance_order_sync.go:210-290`

```go
posBuilder := store.NewPositionBuilder(st.Position())

for _, trade := range allTrades {
    // Determine action (open_long, open_short, close_long, close_short)
    orderAction := determineOrderAction(trade)

    // Process trade through PositionBuilder
    err := posBuilder.ProcessTrade(
        traderID,
        exchangeID,
        exchangeType,
        trade.Symbol,
        trade.Side,
        orderAction,
        trade.Quantity,
        trade.Price,
        trade.Commission,
        trade.RealizedPnl,
        trade.Time,
        trade.OrderID,
    )

    // Record order
    orderRecord := &store.TraderOrder{...}
    st.Order().CreateOrder(orderRecord)

    // Record fill
    fillRecord := &store.TraderFill{...}
    st.Order().CreateFill(fillRecord)
}
```

**Position Builder Logic:**

**Location:** `store/position_builder.go:27-41`

```go
func (pb *PositionBuilder) ProcessTrade(
    traderID, exchangeID, exchangeType, symbol, side, action string,
    quantity, price, fee, realizedPnL float64,
    tradeTimeMs int64,
    orderID string,
) error {
    if strings.HasPrefix(action, "open_") {
        return pb.handleOpen(traderID, exchangeID, exchangeType, symbol, side, quantity, price, fee, tradeTimeMs, orderID)
    } else if strings.HasPrefix(action, "close_") {
        return pb.handleClose(traderID, exchangeID, exchangeType, symbol, side, quantity, price, fee, realizedPnL, tradeTimeMs, orderID)
    }
    return nil
}
```

---

## Position Lifecycle

### 2.1 Position Opening

**Trigger:** Order fill for `open_long` or `open_short`

**Two scenarios:**

#### Scenario A: First Open (No existing position)

**Location:** `store/position_builder.go:58-79`

```go
func (pb *PositionBuilder) handleOpen(...) error {
    existing, err := pb.positionStore.GetOpenPositionBySymbol(traderID, symbol, side)

    if existing == nil {
        // Create new position
        position := &TraderPosition{
            TraderID:           traderID,
            ExchangeID:         exchangeID,
            ExchangeType:       exchangeType,
            Symbol:             symbol,
            Side:               side,
            Quantity:           quantity,
            EntryPrice:         price,
            EntryOrderID:       orderID,
            EntryTime:          tradeTimeMs,
            Leverage:           1,
            Status:             "OPEN",  // ⚠️ OPEN status
            Source:             "sync",
            Fee:                fee,
            CreatedAt:          nowMs,
            UpdatedAt:          nowMs,
        }
        return pb.positionStore.CreateOpenPosition(position)
    }
}
```

**Database Record Created:**

**Location:** `store/position.go:29-53`

```go
type TraderPosition struct {
    ID                 int64   `gorm:"primaryKey;autoIncrement"`
    TraderID           string  `gorm:"column:trader_id;not null;index"`
    ExchangeID         string  `gorm:"column:exchange_id;not null"`
    ExchangeType       string  `gorm:"column:exchange_type;not null"`
    Symbol             string  `gorm:"column:symbol;not null"`
    Side               string  `gorm:"column:side;not null"`           // LONG or SHORT
    EntryQuantity      float64 `gorm:"column:entry_quantity;default:0"`
    Quantity           float64 `gorm:"column:quantity;not null"`       // Current quantity
    EntryPrice         float64 `gorm:"column:entry_price;not null"`
    EntryOrderID       string  `gorm:"column:entry_order_id"`
    EntryTime          int64   `gorm:"column:entry_time;not null;index"`  // Unix ms UTC
    ExitPrice          float64 `gorm:"column:exit_price;default:0"`
    ExitOrderID        string  `gorm:"column:exit_order_id;default:''"`
    ExitTime           int64   `gorm:"column:exit_time;index"`         // Unix ms UTC, 0 = not set
    RealizedPnL        float64 `gorm:"column:realized_pnl;default:0"`
    Fee                float64 `gorm:"column:fee;default:0"`
    Leverage           int     `gorm:"column:leverage;default:1"`
    Status             string  `gorm:"column:status;default:OPEN;index"`  // ⚠️ KEY FIELD
    CloseReason        string  `gorm:"column:close_reason;default:''"`
    Source             string  `gorm:"column:source;default:system"`
    CreatedAt          int64   `gorm:"column:created_at"`             // Unix ms UTC
    UpdatedAt          int64   `gorm:"column:updated_at"`             // Unix ms UTC
}
```

#### Scenario B: Position Averaging (Adding to existing position)

**Location:** `store/position_builder.go:81-93`

```go
if existing != nil {
    // Merge: Calculate weighted average entry price
    logger.Infof("  📊 Averaging position: %s %s %.6f @ %.2f + %.6f @ %.2f",
        symbol, side, existing.Quantity, existing.EntryPrice, quantity, price)

    // Calculate new weighted average price
    newQty := existing.Quantity + quantity
    newEntryPrice := (existing.EntryPrice * existing.Quantity + price * quantity) / newQty

    return pb.positionStore.UpdatePositionQuantityAndPrice(existing.ID, quantity, price, fee)
}
```

**Update Logic:**

**Location:** `store/position.go:144-170`

```go
func (s *PositionStore) UpdatePositionQuantityAndPrice(id int64, addQty float64, addPrice float64, addFee float64) error {
    var pos TraderPosition
    s.db.First(&pos, id)

    // Calculate new values
    newQty := math.Round((pos.Quantity + addQty) * 10000) / 10000
    newEntryQty := math.Round((pos.EntryQuantity + addQty) * 10000) / 10000
    newEntryPrice := (pos.EntryPrice*pos.Quantity + addPrice*addQty) / newQty
    newEntryPrice = math.Round(newEntryPrice*100) / 100
    newFee := pos.Fee + addFee

    return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
        "quantity":       newQty,
        "entry_quantity": newEntryQty,
        "entry_price":    newEntryPrice,
        "fee":            newFee,
        "updated_at":     time.Now().UTC().UnixMilli(),
    }).Error
}
```

---

### 2.2 Position Monitoring

**Trigger:** Every trading cycle when building context for AI

**Location:** `trader/auto_trader.go:727-820`

```go
func (at *AutoTrader) buildTradingContext() (*kernel.Context, error) {
    // ... get balance ...

    // 2. Get position information
    positions, err := at.trader.GetPositions()
    if err != nil {
        return nil, fmt.Errorf("failed to get positions: %w", err)
    }

    var positionInfos []kernel.PositionInfo
    totalMarginUsed := 0.0

    // Current position key set (for cleaning up closed position records)
    currentPositionKeys := make(map[string]bool)

    for _, pos := range positions {
        symbol := pos["symbol"].(string)
        side := pos["side"].(string)
        entryPrice := pos["entryPrice"].(float64)
        markPrice := pos["markPrice"].(float64)
        quantity := pos["positionAmt"].(float64)

        // Skip closed positions (quantity = 0)
        if quantity == 0 {
            continue
        }

        unrealizedPnl := pos["unRealizedProfit"].(float64)
        leverage := int(pos["leverage"].(float64))

        // Calculate margin used
        marginUsed := (quantity * markPrice) / float64(leverage)
        totalMarginUsed += marginUsed

        // Calculate P&L percentage
        pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

        // Get position open time
        posKey := symbol + "_" + side
        currentPositionKeys[posKey] = true

        var updateTime int64
        if dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, side); err == nil && dbPos != nil {
            updateTime = dbPos.EntryTime
        }

        // Track peak P&L for trailing stop
        at.UpdatePeakPnL(symbol, side, pnlPct)

        positionInfos = append(positionInfos, kernel.PositionInfo{
            Symbol:           symbol,
            Side:             side,
            EntryPrice:       entryPrice,
            MarkPrice:        markPrice,
            Quantity:         quantity,
            Leverage:         leverage,
            UnrealizedPnL:    unrealizedPnl,
            UnrealizedPnLPct: pnlPct,
            PeakPnLPct:       at.GetPeakPnL(symbol, side),
            LiquidationPrice: pos["liquidationPrice"].(float64),
            MarginUsed:       marginUsed,
            UpdateTime:       updateTime,
        })
    }

    // Clean up closed positions in database
    at.cleanupClosedPositions(currentPositionKeys)

    return &kernel.Context{
        Account: kernel.AccountInfo{
            // ... balance info ...
            Positions:   positionInfos,
            PositionCount: len(positionInfos),
            MarginUsed:   totalMarginUsed,
        },
    }, nil
}
```

**What happens:**
1. Fetch current positions from exchange
2. Skip positions with `quantity = 0` (already closed)
3. Calculate unrealized P&L and margin used
4. Track peak P&L for trailing stop calculations
5. Update database with latest position state
6. Provide position data to AI for decision-making

---

### 2.3 Position Closure

**Trigger:** AI decides to close OR stop-loss/take-profit triggered OR drawdown monitoring

**Three closure mechanisms:**

#### Mechanism A: AI-Driven Closure

**Location:** `trader/auto_trader.go:1264-1326`

```go
func (at *AutoTrader) executeCloseLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
    logger.Infof("  🔄 Close long: %s", decision.Symbol)

    // Get entry price and quantity from local database
    if at.store != nil {
        if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "LONG"); err == nil && openPos != nil {
            quantity = openPos.Quantity
            entryPrice = openPos.EntryPrice
            logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
        }
    }

    // Close position
    order, err := at.trader.CloseLong(decision.Symbol, 0)  // 0 = close all
    if err != nil {
        return err
    }

    // Record order to database and poll for confirmation
    at.recordAndConfirmOrder(order, decision.Symbol, "close_long", quantity, marketData.CurrentPrice, 0, entryPrice)

    logger.Infof("  ✓ Position closed successfully")
    return nil
}
```

**Exchange Implementation:**

**Location:** `trader/binance_futures.go:487-520`

```go
func (t *FuturesTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
    symbol = market.Normalize(symbol)

    // Get current position to determine quantity
    if quantity == 0 {
        positions, err := t.GetPositions()
        if err != nil {
            return nil, err
        }
        for _, pos := range positions {
            if pos["symbol"] == symbol && pos["side"] == "long" {
                if amt, ok := pos["positionAmt"].(float64); ok {
                    quantity = amt
                }
                break
            }
        }
    }

    // Close position with SELL market order
    order, err := t.client.NewCreateOrderService().
        Symbol(symbol).
        Side("SELL").
        Type("MARKET").
        Quantity(fmt.Sprintf("%.6f", quantity)).
        PositionSide("LONG").
        ReduceOnly(true).  // ⚠️ Ensure we only close, not open new position
        Do(context.Background())

    return map[string]interface{}{
        "orderId": order.OrderID,
        "symbol":  symbol,
    }, nil
}
```

#### Mechanism B: Drawdown-Based Closure (Background)

**Location:** `trader/auto_trader.go:1750-1824`

```go
func (at *AutoTrader) checkPositionDrawdown() {
    positions, err := at.trader.GetPositions()
    if err != nil {
        return
    }

    for _, pos := range positions {
        symbol := pos["symbol"].(string)
        side := pos["side"].(string)
        entryPrice := pos["entryPrice"].(float64)
        markPrice := pos["markPrice"].(float64)
        leverage := int(pos["leverage"].(float64))

        // Calculate current P&L percentage
        var currentPnLPct float64
        if side == "long" {
            currentPnLPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
        } else {
            currentPnLPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
        }

        // Get peak P&L from cache
        posKey := symbol + "_" + side
        peakPnLPct, exists := at.peakPnLCache[posKey]

        if !exists {
            peakPnLPct = currentPnLPct
            at.UpdatePeakPnL(symbol, side, currentPnLPct)
        } else {
            at.UpdatePeakPnL(symbol, side, currentPnLPct)
        }

        // Calculate drawdown from peak
        var drawdownPct float64
        if peakPnLPct > 0 && currentPnLPct < peakPnLPct {
            drawdownPct = ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
        }

        // Check close condition: profit > 5% AND drawdown >= 40%
        if currentPnLPct > 5.0 && drawdownPct >= 40.0 {
            logger.Infof("🚨 Drawdown close position condition triggered: %s %s | Current: %.2f%% | Peak: %.2f%% | Drawdown: %.2f%%",
                symbol, side, currentPnLPct, peakPnLPct, drawdownPct)

            // Execute close position
            if err := at.emergencyClosePosition(symbol, side); err != nil {
                logger.Infof("❌ Drawdown close position failed (%s %s): %v", symbol, side, err)
            } else {
                logger.Infof("✅ Drawdown close position succeeded: %s %s", symbol, side)
                at.ClearPeakPnLCache(symbol, side)
            }
        }
    }
}
```

**Emergency Close:**

**Location:** `trader/auto_trader.go:1827-1846`

```go
func (at *AutoTrader) emergencyClosePosition(symbol, side string) error {
    switch side {
    case "long":
        order, err := at.trader.CloseLong(symbol, 0)  // 0 = close all
        if err != nil {
            return err
        }
        logger.Infof("✅ Emergency close long position succeeded, order ID: %v", order["orderId"])
    case "short":
        order, err := at.trader.CloseShort(symbol, 0)
        if err != nil {
            return err
        }
        logger.Infof("✅ Emergency close short position succeeded, order ID: %v", order["orderId"])
    }
    return nil
}
```

#### Mechanism C: Exchange-Level Stop-Loss Order

**Location:** `trader/auto_trader.go:1137-1142`

```go
// Set stop loss and take profit immediately after opening
if err := at.trader.SetStopLoss(decision.Symbol, "LONG", quantity, decision.StopLoss); err != nil {
    logger.Infof("  ⚠ Failed to set stop loss: %v", err)
}
if err := at.trader.SetTakeProfit(decision.Symbol, "LONG", quantity, decision.TakeProfit); err != nil {
    logger.Infof("  ⚠ Failed to set take profit: %v", err)
}
```

**Exchange Implementation (Binance):**

**Location:** `trader/binance_futures.go:985-1015`

```go
func (t *FuturesTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
    // Create stop-loss order using new Algo Order API
    order, err := t.client.NewCreateOrderService().
        Symbol(symbol).
        Side("SELL").
        Type("STOP").
        Quantity(fmt.Sprintf("%.6f", quantity)).
        PositionSide(positionSide).
        StopPrice(fmt.Sprintf("%.4f", stopPrice)).
        Price(fmt.Sprintf("%.4f", stopPrice)).  // Limit price same as stop for market-like fill
        TimeInForce("GTC").
        WorkingType("MARK_PRICE").
        PriceProtect(true).
        Do(context.Background())

    return err
}
```

**Note:** Stop-loss orders are **exchange-side orders** that execute automatically when price is hit, independent of the NOFX system.

---

### 2.4 Position History

**Trigger:** After position closure

**Actions:**

#### A. Update Position Record

**Location:** `store/position_builder.go:129-173`

```go
func (pb *PositionBuilder) handleClose(...) error {
    position, err := pb.positionStore.GetOpenPositionBySymbol(traderID, symbol, side)

    if position == nil {
        logger.Infof("  ⚠️  No matching open position for %s %s (orderID: %s), skipping", symbol, side, orderID)
        return nil
    }

    const QUANTITY_TOLERANCE = 0.0001

    // Calculate realized PnL if not provided
    if realizedPnL == 0 && position.EntryPrice > 0 {
        if side == "LONG" {
            realizedPnL = (price - position.EntryPrice) * quantity
        } else {
            realizedPnL = (position.EntryPrice - price) * quantity
        }
        realizedPnL = math.Round(realizedPnL*100) / 100
    }

    if quantity < position.Quantity-QUANTITY_TOLERANCE {
        // Partial close: reduce quantity
        return pb.positionStore.ReducePositionQuantity(position.ID, quantity, price, fee, realizedPnL)
    } else {
        // Full close: mark as CLOSED
        closeQty := quantity
        if quantity > position.Quantity {
            closeQty = position.Quantity  // Avoid over-close
        }

        // Calculate final weighted average exit price
        closedBefore := position.EntryQuantity - position.Quantity
        totalClosed := closedBefore + closeQty
        finalExitPrice := (position.ExitPrice*closedBefore + price*closeQty) / totalClosed

        totalPnL := position.RealizedPnL + realizedPnL
        totalFee := position.Fee + fee

        logger.Infof("  ✅ Full close: %s %s %.6f @ %.2f (avg exit: %.2f, entry: %.2f, PnL: %.2f)",
            symbol, side, closeQty, price, finalExitPrice, position.EntryPrice, totalPnL)

        return pb.positionStore.ClosePositionFully(
            position.ID,
            finalExitPrice,
            orderID,
            tradeTimeMs,
            totalPnL,
            totalFee,
            "sync",
        )
    }
}
```

#### B. Database Update

**Full Close:**

**Location:** `store/position.go:232-254`

```go
func (s *PositionStore) ClosePositionFully(id int64, exitPrice float64, exitOrderID string, exitTimeMs int64, totalRealizedPnL float64, totalFee float64, closeReason string) error {
    var pos TraderPosition
    s.db.First(&pos, id)

    quantity := pos.EntryQuantity  // Use entry quantity as final

    return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
        "quantity":       quantity,
        "exit_price":     exitPrice,
        "exit_order_id":  exitOrderID,
        "exit_time":      exitTimeMs,
        "realized_pnl":   totalRealizedPnL,
        "fee":            totalFee,
        "status":         "CLOSED",  // ⚠️ Status change
        "close_reason":   closeReason,
        "updated_at":     time.Now().UTC().UnixMilli(),
    }).Error
}
```

**Partial Close:**

**Location:** `store/position.go:174-218`

```go
func (s *PositionStore) ReducePositionQuantity(id int64, reduceQty float64, exitPrice float64, addFee float64, addPnL float64) error {
    var pos TraderPosition
    s.db.First(&pos, id)

    newQty := math.Round((pos.Quantity - reduceQty) * 10000) / 10000
    newFee := pos.Fee + addFee
    newPnL := pos.RealizedPnL + addPnL

    closedQty := pos.EntryQuantity - pos.Quantity
    newClosedQty := closedQty + reduceQty

    // Calculate weighted average exit price
    var newExitPrice float64
    if newClosedQty > 0 {
        newExitPrice = (pos.ExitPrice*closedQty + exitPrice*reduceQty) / newClosedQty
        newExitPrice = math.Round(newExitPrice*100) / 100
    }

    const QUANTITY_TOLERANCE = 0.0001
    if newQty <= QUANTITY_TOLERANCE {
        // Auto-close: quantity reduced to ~0
        return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
            "quantity":     0,
            "fee":          newFee,
            "exit_price":   newExitPrice,
            "realized_pnl": newPnL,
            "status":       "CLOSED",  // ⚠️ Auto-close on full reduction
            "exit_time":    time.Now().UTC().UnixMilli(),
            "close_reason": "sync",
            "updated_at":   time.Now().UTC().UnixMilli(),
        }).Error
    }

    // Partial close: keep OPEN status, update quantity
    return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
        "quantity":     newQty,
        "fee":          newFee,
        "exit_price":   newExitPrice,
        "realized_pnl": newPnL,
        "updated_at":   time.Now().UTC().UnixMilli(),
    }).Error
}
```

#### C. Clear Peak P&L Cache

**Location:** `trader/auto_trader.go:1816` (after successful close)

```go
// Clear cache for this position after closing
at.ClearPeakPnLCache(symbol, side)
```

---

## Data Models

### Order Status Values

| Status | Description | Code Evidence |
|--------|-------------|---------------|
| `NEW` | Order created, not yet filled | `store/order.go:28` |
| `PARTIALLY_FILLED` | Order partially filled | `trader/binance_futures.go:902` |
| `FILLED` | Order completely filled | `store/order.go:178` |
| `CANCELED` | Order canceled by user | `trader/auto_trader.go:1971` |
| `EXPIRED` | Order expired | `trader/auto_trader.go:1971` |
| `REJECTED` | Order rejected by exchange | `trader/auto_trader.go:1971` |

### Position Status Values

| Status | Description | Code Evidence |
|--------|-------------|---------------|
| `OPEN` | Position currently active | `store/position.go:48` |
| `CLOSED` | Position closed, P&L realized | `store/position.go:250` |

### Order Action Values

| Action | Side | Position Side | Description |
|--------|------|---------------|-------------|
| `open_long` | BUY | LONG | Open long position |
| `open_short` | SELL | SHORT | Open short position |
| `close_long` | SELL | LONG | Close long position |
| `close_short` | BUY | SHORT | Close short position |

**Code Evidence:** `store/order.go:38`, `trader/auto_trader.go:2063-2068`

---

## Key Files Reference

### Order Lifecycle

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| Order Execution | `trader/auto_trader.go:488-665` | `runCycle()` |
| Open Long | `trader/auto_trader.go:1040-1145` | `executeOpenLongWithRecord()` |
| Open Short | `trader/auto_trader.go:1147-1262` | `executeOpenShortWithRecord()` |
| Close Long | `trader/auto_trader.go:1264-1326` | `executeCloseLongWithRecord()` |
| Close Short | `trader/auto_trader.go:1328-1390` | `executeCloseShortWithRecord()` |
| Order Recording | `trader/auto_trader.go:1887-2004` | `recordAndConfirmOrder()` |
| Order Creation | `trader/auto_trader.go:2056-2103` | `createOrderRecord()` |
| Fill Recording | `trader/auto_trader.go:2105-2149` | `recordOrderFill()` |
| Position Recording | `trader/auto_trader.go:2006-2054` | `recordPositionChange()` |
| Order Storage | `store/order.go:11-424` | `OrderStore` methods |
| Binance Trader | `trader/binance_futures.go:447-520` | `OpenLong()`, `CloseLong()` |
| Order Sync | `trader/binance_order_sync.go:23-350` | `SyncOrdersFromBinance()` |

### Position Lifecycle

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| Position Monitoring | `trader/auto_trader.go:727-820` | `buildTradingContext()` (positions section) |
| Drawdown Monitor | `trader/auto_trader.go:1750-1824` | `checkPositionDrawdown()` |
| Emergency Close | `trader/auto_trader.go:1827-1846` | `emergencyClosePosition()` |
| Peak P&L Tracking | `trader/auto_trader.go:1861-1876` | `UpdatePeakPnL()` |
| Position Builder | `store/position_builder.go:1-181` | `ProcessTrade()`, `handleOpen()`, `handleClose()` |
| Position Storage | `store/position.go:1-1164` | `PositionStore` methods |
| Position Create | `store/position.go:120-127` | `Create()` |
| Position Update | `store/position.go:144-170` | `UpdatePositionQuantityAndPrice()` |
| Position Reduce | `store/position.go:174-218` | `ReducePositionQuantity()` |
| Position Full Close | `store/position.go:232-254` | `ClosePositionFully()` |

### Data Models

| Model | File Path | Lines |
|-------|-----------|-------|
| `TraderOrder` | `store/order.go:13-43` | Order record |
| `TraderFill` | `store/order.go:52-75` | Fill/trade record |
| `TraderPosition` | `store/position.go:29-53` | Position record |

---

## Flow Diagrams

### Order Flow (Exchanges WITH OrderSync)

```
AI Decision
    ↓
Execute Open/Close
    ↓
Submit Order to Exchange
    ↓
Receive Order ID
    ↓
Skip Recording (return early)
    ↓
Background OrderSync Runs (every ~5 min)
    ↓
Fetch Trades from Exchange API
    ↓
Process Trades through PositionBuilder
    ↓
Create Order Records (FILLED status)
    ↓
Create Fill Records
    ↓
Create/Update Position Records
```

### Order Flow (Exchanges WITHOUT OrderSync)

```
AI Decision
    ↓
Execute Open/Close
    ↓
Submit Order to Exchange
    ↓
Receive Order ID
    ↓
Create Order Record (status: NEW)
    ↓
Poll OrderStatus (max 5 times, 500ms interval)
    ↓
Order Filled
    ↓
Update Order Record (status: FILLED)
    ↓
Create Fill Record
    ↓
Create/Update Position Record
```

### Position Lifecycle

```
Order Fill (open_long/open_short)
    ↓
PositionBuilder.ProcessTrade()
    ↓
handleOpen() called
    ↓
Check existing OPEN position
    ↓
┌─────────────────┬─────────────────┐
│ No existing     │ Existing found  │
│ position        │                 │
├─────────────────┼─────────────────┤
│ Create new      │ Average into    │
│ position record │ existing        │
│ (status: OPEN)  │ (weighted avg   │
│                 │  entry price)   │
└─────────────────┴─────────────────┘
    ↓
Position appears in GetPositions()
    ↓
Monitored every cycle (buildTradingContext)
    ↓
Peak P&L tracked
    ↓
AI Decision / Stop-Loss / Drawdown
    ↓
Order Fill (close_long/close_short)
    ↓
PositionBuilder.ProcessTrade()
    ↓
handleClose() called
    ↓
Calculate realized P&L
    ↓
┌─────────────────┬─────────────────┐
│ Partial close   │ Full close      │
│ (qty < total)   │ (qty ≈ total)   │
├─────────────────┼─────────────────┤
│ Reduce quantity │ Set status to   │
│ (keep OPEN)     │ CLOSED          │
│ Update exit     │ Record final    │
│ price (weighted)│ exit price, P&L │
└─────────────────┴─────────────────┘
    ↓
Position history saved
```

---

## Summary

The NOFX system implements a **robust, multi-layered approach** to order and position lifecycle management:

### Order Lifecycle Highlights:

1. **Dual-path recording** - Exchanges with OrderSync use background sync for accuracy; others use immediate polling
2. **Status tracking** - Orders progress through NEW → FILLED states with timestamps
3. **Fill records** - Every fill creates a separate record with exact price/quantity/fee
4. **Background sync** - Periodic sync from exchange APIs ensures data consistency
5. **Incremental sync** - Uses trade IDs and timestamps for efficient incremental updates

### Position Lifecycle Highlights:

1. **Position averaging** - Multiple opens on same symbol/side are merged with weighted average entry price
2. **Partial closes** - Supports reducing position quantity while keeping status OPEN
3. **P&L calculation** - Realized P&L calculated on close, considering entry/exit prices and leverage
4. **Multi-mechanism closure** - AI-driven, stop-loss orders, and drawdown monitoring
5. **Peak tracking** - Tracks peak P&L for trailing stop calculations

### Data Consistency:

- **OrderSync** ensures trades are fetched from exchange API
- **PositionBuilder** handles complex scenarios (averaging, partial closes)
- **Database constraints** prevent duplicate records (unique indexes on exchange_order_id, exchange_trade_id)
- **Tolerance-based checks** handle floating-point precision issues (QUANTITY_TOLERANCE = 0.0001)

---

**Generated:** 2026-01-29
**Version:** 1.0
