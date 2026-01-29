# NOFX Trading System - Architecture Design

## 🏗️ Architecture Pattern: Layered Microkernel

The system uses a **layered architecture** with a **microkernel-style plugin system**, enabling extensibility through well-defined interfaces.

```
┌─────────────────────────────────────────────────────────────────┐
│                   PRESENTATION LAYER                            │
│              React Frontend (TypeScript + Vite)                │
└─────────────────────────────────────────────────────────────────┘
                              ↓ HTTP/WebSocket
┌─────────────────────────────────────────────────────────────────┐
│                      API GATEWAY                                │
│                   Gin HTTP Server (Go)                         │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                   APPLICATION LAYER                             │
│   TraderManager │ BacktestManager │ DebateEngine               │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                   BUSINESS LOGIC LAYER                          │
│   StrategyEngine │ AutoTrader │ Exchange Interface              │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                  INFRASTRUCTURE LAYER                           │
│   Market Data │ AI Client │ Database (GORM) │ Crypto           │
└─────────────────────────────────────────────────────────────────┘
```

---

## 📁 Project Structure

```
nofx/
├── main.go                    # Application entry point
├── config/                    # Global configuration
├── api/                       # REST API layer (Gin)
├── trader/                    # Trading execution engines
├── kernel/                    # Core strategy & AI decision engine
├── market/                    # Market data acquisition & indicators
├── mcp/                       # AI client abstraction layer
├── manager/                   # Multi-trader orchestration
├── store/                     # Database persistence (GORM)
├── backtest/                  # Historical simulation engine
├── debate/                    # Multi-AI debate arena
├── provider/                  # External data providers
├── auth/                      # JWT authentication
├── crypto/                    # Encryption service
├── logger/                    # Logging infrastructure
├── web/                       # React frontend
└── docs/                      # Documentation
```

---

## 🎯 Core Components

### 1. Strategy Engine (`kernel/`)

The heart of the trading system that orchestrates AI decision-making.

**Key Responsibilities:**
- Fetches and formats market data (K-lines, indicators)
- Constructs AI prompts with bilingual schema
- Calls AI models via unified MCP interface
- Parses and validates AI decisions
- Enforces risk controls

**Data Flow:**
```
Market Data → Context Building → Prompt → AI → Response → Parsed Decision → Execution
```

**Key Files:**
- `kernel/engine.go` - Main execution engine
- `kernel/prompt_builder.go` - AI prompt construction
- `kernel/schema.go` - Bilingual data dictionary
- `kernel/grid_engine.go` - Grid trading specialist

### 2. Trader Interface (`trader/`)

Unified abstraction over multiple exchanges (CEX & DEX).

```go
type Trader interface {
    GetBalance() (map[string]interface{}, error)
    GetPositions() ([]map[string]interface{}, error)
    OpenLong(symbol, quantity, leverage) (order, error)
    OpenShort(symbol, quantity, leverage) (order, error)
    CloseLong(symbol, quantity) (order, error)
    CloseShort(symbol, quantity) (order, error)
    SetStopLoss(symbol, side, quantity, price) error
    SetTakeProfit(symbol, side, quantity, price) error
}
```

**Supported Exchanges:**
- **CEX**: Binance, Bybit, OKX, Bitget
- **DEX**: Hyperliquid, Aster, Lighter

**Key Files:**
- `trader/auto_trader.go` - Autonomous trading agent
- `trader/binance_futures.go` - Binance implementation
- `trader/hyperliquid_trader.go` - Hyperliquid DEX
- `trader/bybit_trader.go` - Bybit exchange
- `trader/okx_trader.go` - OKX exchange
- `trader/aster_trader.go` - Aster DEX
- `trader/lighter_trader_v2_orders.go` - Lighter DEX
- `trader/bitget_trader.go` - Bitget exchange

### 3. Auto Trader (`trader/auto_trader.go`)

Autonomous trading agent that runs the complete trading cycle.

**Trading Loop:**
```
1. Timer Trigger (default 3 min)
2. Fetch Market Data (K-lines, indicators)
3. Build Trading Context (positions, account, candidates)
4. Call AI via StrategyEngine
5. Parse Decision (open/close/hold with parameters)
6. Validate Risk Controls
7. Execute Trade via Trader interface
8. Log Decision to Database
9. Update Equity Snapshot
```

**Key Features:**
- Multi-timeframe analysis
- Peak P&L tracking for trailing stops
- Daily loss limits with pause duration
- Position-first-seen-time tracking
- Balance synchronization from exchange

### 4. Market Data Layer (`market/`)

Data acquisition and technical indicator calculation.

**Data Sources:**
- **CoinAnk API** (primary) - K-lines and OHLCV data
- **NofxOS API** (supplementary) - AI500 pool, OI rankings

**Indicators:**
- EMA (multiple periods)
- MACD
- RSI (multiple periods)
- ATR
- Bollinger Bands
- BOX (support/resistance)
- Volume & Open Interest
- Funding Rate

**Key Files:**
- `market/data.go` - Main data structure
- `market/fetcher.go` - API integration
- `market/indicators.go` - Technical calculations

### 5. AI Client Layer (`mcp/`)

Unified abstraction supporting 7 AI providers.

**Supported AI:**
- DeepSeek (default, cost-effective)
- Qwen (Alibaba Cloud)
- OpenAI (GPT models)
- Anthropic (Claude)
- Google (Gemini)
- xAI (Grok)
- Moonshot (Kimi)

```go
type AIClient interface {
    SetAPIKey(apiKey, customURL, customModel string)
    SetTimeout(timeout time.Duration)
    CallWithMessages(systemPrompt, userPrompt string) (string, error)
    CallWithRequest(req *Request) (string, error)
}
```

**Key Features:**
- Template method pattern with hooks
- Retry logic with exponential backoff
- Token usage tracking for analytics
- Builder pattern for advanced requests

**Key Files:**
- `mcp/deepseek_client.go`
- `mcp/qwen_client.go`
- `mcp/openai_client.go`
- `mcp/claude_client.go`
- `mcp/gemini_client.go`
- `mcp/grok_client.go`
- `mcp/kimi_client.go`

### 6. Storage Layer (`store/`)

Database persistence with GORM.

**Databases:**
- **SQLite** (default) - Zero-config, single-file deployment
- **PostgreSQL** (optional) - Production scalability

**Key Models:**
- `Trader` - Trading agent configurations
- `AIModel` - AI provider credentials
- `Exchange` - Exchange API keys
- `Strategy` - Reusable strategy templates
- `Decision` - AI decision logs with chain-of-thought
- `Position` - Active/closed position records
- `Equity` - Equity curve snapshots
- `Backtest` - Backtest runs and results
- `Order` - Trade execution records

**Encryption:**
- AES-256-GCM encryption for API keys
- Optional browser-side encryption via Web Crypto API
- `EncryptedString` type for secure fields

**Key Files:**
- `store/store.go` - Main store initialization
- `store/strategy.go` - Strategy configuration
- `store/position.go` - Position tracking
- `store/decision.go` - Decision logging

### 7. Manager Layer (`manager/`)

Multi-trader orchestration and lifecycle management.

**Responsibilities:**
- Trader lifecycle (start, stop, reload)
- Competition data aggregation
- Auto-restore running traders after restart
- Concurrent account info fetching with timeout
- Top-5 trader leaderboard caching (30s TTL)

**Key Files:**
- `manager/trader_manager.go` - Multi-trader orchestration
- `manager/competition.go` - Competition logic

### 8. Backtest Engine (`backtest/`)

Historical simulation with AI decision replay.

**Features:**
- Multi-symbol, multi-timeframe backtesting
- AI decision caching (replay without re-calling AI)
- Checkpoint/resume support
- Real-time progress streaming via SSE
- Performance metrics (Sharpe, max drawdown, win rate)
- Equity curve visualization data

**Architecture:**
```
Manager → Runner → DataFeed → Account → StrategyEngine → Decision → Execution
                ↓           ↓          ↓
            Storage    Metrics    Events
```

**Key Files:**
- `backtest/manager.go` - Backtest orchestration
- `backtest/runner.go` - Simulation engine
- `backtest/data_feed.go` - Historical data provider
- `backtest/metrics.go` - Performance calculations

### 9. Debate Arena (`debate/`)

Multi-AI collaborative decision making.

**5 AI Personalities:**
1. **Bull** - Optimistic, focuses on upside
2. **Bear** - Pessimistic, focuses on risks
3. **Analyst** - Balanced, data-driven
4. **Contrarian** - Goes against consensus
5. **Risk Manager** - Conservative, capital preservation

**Process:**
1. Market context distributed to all AIs
2. Each AI provides reasoning + decision
3. Weighted voting (configurable weights)
4. Consensus threshold check
5. Optional auto-execution

**Key Files:**
- `debate/engine.go` - Multi-AI orchestration
- `debate/personalities.go` - AI personality definitions

---

## 🔄 Data Flow

### Live Trading Cycle

```
Timer Trigger (ScanInterval, default 3 min)
    ↓
Fetch Market Data
  ├─ market.GetKlines() → CoinAnk API
  ├─ Calculate indicators (EMA, MACD, RSI, ATR, BOLL, BOX)
  └─ Cache funding rates (1-hour TTL)
    ↓
Build Context
  ├─ Get account info (balance, positions)
  ├─ Get candidate coins (AI500, OI Top, static list)
  ├─ Get trading statistics (win rate, PnL)
  ├─ Get recent orders (last 10 trades)
  └─ Build kernel.Context struct
    ↓
Strategy Engine.Execute()
  ├─ Format market data for AI (bilingual schema)
  ├─ Build system prompt (role, standards, process)
  ├─ Build user prompt (context + data + task)
  ├─ Call AI: mcpClient.CallWithMessages()
  └─ Parse response: <decision> JSON extraction
    ↓
Decision Validation
  ├─ Check leverage limits
  ├─ Check position size limits
  ├─ Check daily loss limits
  └─ Verify margin usage
    ↓
Trade Execution
  ├─ trader.OpenLong() / OpenShort() / CloseLong() / CloseShort()
  ├─ Set stop-loss / take-profit
  └─ Handle errors with retry
    ↓
Persistence
  ├─ Log decision to store.Decision (with AI reasoning)
  ├─ Update position records
  ├─ Create equity snapshot
  └─ Sync balance from exchange
    ↓
Next Cycle (wait for ScanInterval)
```

### Web Interaction Flow

```
Browser (React)
   ↓ fetch/axios
API Server (Gin)
   ↓ auth middleware (JWT)
Router Handler
   ↓
Business Logic:
   ├─ TraderManager (trader operations)
   ├─ BacktestManager (simulation)
   ├─ DebateHandler (multi-AI)
   └─ Store (database queries)
   ↓
Response (JSON)
   ↓
Browser (SWR cache → UI update)
```

### Backtest Flow

```
API Request: POST /api/backtest/start
   ↓
BacktestManager.Start()
   ↓
Create Runner with config
   ↓
Runner.Start()
   ├─ Load historical data (DataFeed)
   ├─ Initialize Account (virtual balance)
   ├─ Loop through time series:
   │   ├─ Get market snapshot at timestamp
   │   ├─ Build context (virtual positions)
   │   ├─ Call AI (with caching)
   │   ├─ Execute decision (virtual)
   │   ├─ Update account (virtual PnL)
   │   ├─ Record trade event
   │   └─ Save checkpoint (every N cycles)
   └─ Calculate metrics (Sharpe, drawdown, etc.)
   ↓
Stream progress via SSE
   ↓
Finalize: Save results, equity curve, trade list
```

---

## 🛠️ Technology Stack

### Backend (Go)

| Component | Technology | Version |
|-----------|-----------|---------|
| Language | Go | 1.25.3 |
| Web Framework | Gin | v1.11.0 |
| Database ORM | GORM | v1.31.1 |
| Databases | SQLite, PostgreSQL | - |
| Crypto | ethereum/go-ethereum | v1.16.7 |
| Authentication | golang-jwt/jwt | v5.2.0 |
| Logging | zerolog, logrus | v1.34.0, v1.9.3 |
| Exchange SDKs | adshao/go-binance, sonirico/vago | v2.8.9, v0.10.0 |
| HTTP Client | quic-go/quic-go | v0.54.0 |
| Encryption | golang.org/x/crypto | v0.42.0 |

### Frontend (React/TypeScript)

| Component | Technology | Version |
|-----------|-----------|---------|
| Language | TypeScript | 5.8.3 |
| Framework | React | 18.3.1 |
| Build Tool | Vite | 6.0.7 |
| State Management | Zustand | 5.0.2 |
| Data Fetching | SWR | 2.2.5 |
| Routing | React Router DOM | 7.9.5 |
| Charts | Lightweight Charts, Recharts | 5.1.0, 2.15.2 |
| UI Components | Radix UI | - |
| Styling | TailwindCSS | 3.4.17 |
| Animations | Framer Motion | 12.23.24 |
| HTTP Client | Axios | 1.13.2 |
| Math Rendering | KaTeX | 0.16.27 |

### External Dependencies

| Category | Services |
|----------|----------|
| **AI Providers** | DeepSeek, Qwen, OpenAI, Claude, Gemini, Grok, Kimi |
| **Exchanges (CEX)** | Binance, Bybit, OKX, Bitget |
| **Exchanges (DEX)** | Hyperliquid, Aster DEX, Lighter |
| **Data Providers** | CoinAnk (K-lines), NofxOS (AI500, OI rankings), Alpaca (US stocks), TwelveData (forex/metals) |
| **Deployment** | Docker, Railway (PaaS) |

---

## 🎨 Key Design Patterns

1. **Strategy Pattern** - Multiple AI models interchangeable via `AIClient` interface
2. **Adapter Pattern** - `GridTraderAdapter` adapts basic traders to grid trading
3. **Factory Pattern** - Exchange-specific trader creation in `trader.NewAutoTrader()`
4. **Repository Pattern** - GORM-based store layer abstracts database operations
5. **Observer Pattern** - SSE streaming for real-time backtest updates
6. **Template Method** - `StrategyEngine` defines trading decision workflow
7. **Facade Pattern** - `TraderManager` simplifies multi-trader orchestration
8. **Builder Pattern** - `kernel.PromptBuilder` constructs AI prompts

---

## 🎯 Key Design Decisions

### Why Layered Architecture?

- **Separation of Concerns**: Each layer has distinct responsibilities
- **Testability**: Layers can be tested in isolation
- **Maintainability**: Changes in one layer don't cascade
- **Scalability**: Layers can be scaled independently (future microservices)

### Why Microkernel Pattern?

- **Extensibility**: New exchanges, AI models, indicators can be added without core changes
- **Plug-and-Play**: `Trader` and `AIClient` interfaces enable swapping components
- **Modularity**: Clear boundaries between core engine and extensions

### Why Go for Backend?

- **Performance**: Compiled, low latency for trading decisions
- **Concurrency**: Goroutines for parallel trader execution
- **Type Safety**: Compile-time error detection
- **Deployment**: Single binary, easy containerization

### Why React + TypeScript?

- **Developer Experience**: Rich ecosystem, fast iteration
- **Type Safety**: TypeScript prevents runtime errors
- **Real-time UI**: Efficient updates with virtual DOM
- **Component Reusability**: Modular UI components

### Unified Interfaces

```go
// All exchanges implement this
type Trader interface { ... }

// All AI providers implement this
type AIClient interface { ... }
```

**Benefit**: Add new exchanges/AI without changing core logic

### Bilingual Schema

- All field names defined in Chinese and English
- Ensures AI understanding regardless of model language
- Located in `kernel/schema.go`

---

## 📊 System Stats

- **103 Go backend files**
- **185 TypeScript/React frontend files**
- **~100,000+ lines of code**
- **7 AI providers supported**
- **7 exchanges supported**
- **4 asset classes** (crypto, US stocks, forex, metals)

---

## 💪 Strengths

1. **Modularity** - Clear boundaries, easy to extend
2. **Multi-AI** - Not locked into single provider
3. **Multi-Exchange** - Diversified execution options
4. **Type Safety** - TypeScript + Go prevent errors
5. **Testability** - Interfaces enable mocking
6. **Real-time** - SSE streaming for live updates
7. **Zero-Config** - SQLite + Docker for easy setup
8. **Bilingual** - Chinese and English support

---

## ⚖️ Trade-Offs

1. **Complexity** - Layered architecture adds indirection
2. **Memory** - Multiple traders + AI clients are resource-intensive
3. **Latency** - AI API calls add 1-5 seconds per decision
4. **Single-Process** - Currently monolithic (could microservice later)
5. **SQLite Limits** - Not for high-concurrency (use PostgreSQL)

---

## 🎯 Use Cases

### ✅ Recommended

- AI model comparison (which AI trades better?)
- Strategy backtesting (historical performance)
- Learning (how do AIs analyze markets?)
- Small-scale live trading (<$10K account)

### ❌ Not Recommended

- High-frequency trading (AI latency too high)
- Large-scale production (single-process architecture)
- Fully automated hands-off trading (requires monitoring)

---

## 📝 Summary

NOFX is a **well-architected, production-grade AI trading system** with:

- **Clear separation of concerns** across 6 architectural layers
- **Plugin-style extensibility** via interfaces (Trader, AIClient)
- **Real-time trading capabilities** with sub-minute decision cycles
- **Comprehensive backtesting** with AI decision caching
- **Multi-AI collaboration** via Debate Arena
- **Enterprise-grade features**: encryption, authentication, persistence
- **Developer-friendly**: 100K+ LOC, well-documented, type-safe

The system successfully balances **complexity** (multiple exchanges, AI models, asset classes) with **maintainability** (layered architecture, interfaces, tests). It's designed for **research and experimentation** while being robust enough for **live trading with small capital**.

---

## 📚 Key File References

| Component | File Path |
|-----------|-----------|
| Main Entry | `/Users/hcb/Projects/nofx/main.go` |
| API Server | `/Users/hcb/Projects/nofx/api/server.go` |
| Auto Trader | `/Users/hcb/Projects/nofx/trader/auto_trader.go` |
| Strategy Engine | `/Users/hcb/Projects/nofx/kernel/engine.go` |
| Prompt Builder | `/Users/hcb/Projects/nofx/kernel/prompt_builder.go` |
| Schema Definition | `/Users/hcb/Projects/nofx/kernel/schema.go` |
| Store | `/Users/hcb/Projects/nofx/store/store.go` |
| Trader Manager | `/Users/hcb/Projects/nofx/manager/trader_manager.go` |
| Backtest Manager | `/Users/hcb/Projects/nofx/backtest/manager.go` |
| Market Data | `/Users/hcb/Projects/nofx/market/data.go` |
| Frontend | `/Users/hcb/Projects/nofx/web/src/` |

---

**Generated:** 2026-01-29
**Version:** 1.0
