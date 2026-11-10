# Product Requirements Document (PRD) — `pzz-bench`

| Metadata | Details |
| :--- | :--- |
| **Product Name** | `pzz-bench` |
| **Version** | `1.0.0-spec` |
| **Target Platform** | Cross-platform Terminal Emulator (Linux, macOS, Windows via WSL2) |
| **Primary Stack** | Go 1.24+, Charmbracelet (`bubbletea`, `lipgloss`, `huh`, `bubbles`), `ntcharts`, `modernc.org/sqlite` |
| **License / Distribution** | Open-Source (MIT), Single Static Binary (Zero CGo) |
| **Core Philosophy** | *Sats-Standard, Local-First, Zero-Lookahead, Aesthetic TUI* |

---

## Implementation status (2026)

The shipped binary follows this spec with these deliberate simplifications:

- **Two workspaces**: `1` Studio and `2` Tools. The optimizer and the market
  cache are sub-tabs of Tools (`[`/`]`), not separate workspaces.
- **Line-run charting**: `ntcharts` renders with its line-run style
  (`╭╮╰╯─│`), not Braille cells. The chart modes (fiat equity, sats stack,
  drawdown) and the oscillator are unchanged.
- **Arrow-only tables**: the result tables navigate with `↑`/`↓`, `PgUp`/`PgDn`
  and `Home`/`End`; the vim row keys were removed.
- **Guided wizard**: `--wizard` runs the same parameters through a two-phase
  `huh` flow (shared form, then strategy parameters) and exports
  `report-*.md` + `trades-*.csv` without starting the TUI.
- **Defaults**: the window starts at `2011-04-28` and ends today; the status
  line lives in the title bar and the footer only lists keys valid for the
  focused zone.

---

## 1. Executive Summary & Product Vision

`pzz-bench` is a deterministic, high-speed, terminal-based (TUI) backtesting and benchmarking engine designed specifically for Bitcoin ($BTC) accumulation and active trading strategies. 

While existing quantitative tools require heavy Python environments, web servers, or complex scripting, `pzz-bench` compiles into a single, dependency-free Go binary. It leverages the **Charmbracelet** ecosystem to deliver a responsive, keyboard-driven, visually refined ("cute" yet information-dense) interface. Its core thesis is simple: **most active strategies fail to beat passive Bitcoin accumulation after fees and slippage; `pzz-bench` exists to mathematically prove or disprove whether a custom indicator-driven strategy generates positive Alpha in both Fiat ($/€) and Satoshis ($\text{sats}$) against a standard DCA or Lump Sum baseline.**

---

## 2. Hard Architectural Decisions (Locked-In)

To avoid scope creep and ambiguity during engineering, the following technical decisions are final:

1. **Zero CGo (`CGO_ENABLED=0`):** All dependencies must be pure Go to guarantee effortless cross-compilation and static binary distribution (e.g., via `pacman` / AUR, Homebrew, or `go install`).
2. **Primary Data Source:** **Binance Public REST API (`/api/v3/klines`)** for `15m`, `1h`, `4h`, `1d`, and `1w` OHLCV data (no API key required, 1,000 candles per batch, history back to August 17, 2017).
3. **Macro Data Fallback:** **Mempool.space REST API (`/api/v1/historical-price`)** for daily macro backtesting prior to August 2017 (down to 2011).
4. **Local Storage Engine:** **SQLite** via `modernc.org/sqlite` stored in XDG-compliant directories (`~/.local/share/pzz-bench/market.db`). Network calls only fetch the missing delta between the latest cached timestamp and `now()`.
5. **Execution Model (Zero Look-Ahead Bias):** Indicators are computed on candle `Close[t]`. Signals generated at `t` are strictly executed at **`Open[t+1]`** plus simulated slippage and exchange fees.
6. **Position Direction:** **Spot Long-Only (`BTC/USDT` or `BTC/EUR`)**. No shorting, no leverage, no liquidation mechanics in v1.0. You are either holding Fiat (Dry Powder), holding BTC, or rebalancing between the two.
7. **TUI Framework:** Charmbracelet `bubbletea` (v1.3+ / v2 compatible architecture), `lipgloss` for layout and Catppuccin Mocha + Bitcoin Orange styling, `huh` for interactive parameter forms, and `NimbleMarkets/ntcharts` for line-run canvas plotting.

---

## 3. System Architecture & Data Pipeline

### 3.1. Directory Structure
```text
pzz-bench/
├── cmd/pzz-bench/
│   └── main.go                 # CLI flags, SQLite init, Bubble Tea program bootstrap
├── internal/
│   ├── config/                 # XDG paths (~/.config/pzz-bench), TOML preset loader/saver
│   ├── data/
│   │   ├── provider.go         # DataProvider interface
│   │   ├── binance.go          # Paginated /api/v3/klines fetcher with rate-limit backoff
│   │   ├── mempool.go          # Pre-2017 daily historical fetcher
│   │   └── sqlite.go           # Upsert & range query on (symbol, timeframe, open_time)
│   ├── engine/
│   │   ├── types.go            # Candle, Order, Trade, PortfolioState, EngineConfig
│   │   ├── indicators.go       # Pre-computed vectorized SMA, EMA, RSI, MACD, Bollinger
│   │   ├── strategy.go         # Strategy interface & built-in strategy implementations
│   │   ├── simulator.go        # Event-driven tick loop (t -> signal -> t+1 fill)
│   │   ├── metrics.go          # Sharpe, Sortino, MaxDD, CAGR, Sats Alpha math
│   │   └── optimizer.go        # Goroutine worker pool for 2D parameter grid search
│   └── tui/
│       ├── app.go              # Root tea.Model, window size listener, view router
│       ├── keymap.go           # Vim-centric keybindings
│       ├── theme.go            # Lip Gloss styles & color palette definitions
│       └── components/
│           ├── form.go         # `huh` configuration sidebar
│           ├── chart.go        # `ntcharts` line-run dual-equity + drawdown renderer
│           ├── osc_chart.go    # `ntcharts` MACD histogram & RSI sub-chart
│           ├── kpi_table.go    # `bubbles/table` comparative metrics view
│           └── heatmap.go      # 2D ANSI grid for parameter optimization results
└── go.mod
```

### 3.2. SQLite Schema (`market.db`)
```sql
CREATE TABLE IF NOT EXISTS candles (
    symbol      TEXT NOT NULL,
    timeframe   TEXT NOT NULL,
    open_time   INTEGER NOT NULL, -- Unix timestamp in milliseconds (UTC)
    open        REAL NOT NULL,
    high        REAL NOT NULL,
    low         REAL NOT NULL,
    close       REAL NOT NULL,
    volume      REAL NOT NULL,
    PRIMARY KEY (symbol, timeframe, open_time)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_lookup 
ON candles (symbol, timeframe, open_time ASC);
```

---

## 4. Quantitative Backtesting Engine Specifications

### 4.1. Input Parameters (`EngineConfig`)
Every benchmark run takes an immutable configuration struct:
* `Symbol`: `"BTCUSDT"` or `"BTCEUR"` (Default: `"BTCUSDT"`)
* `Timeframe`: `"15m"` | `"1h"` | `"4h"` | `"1d"` | `"1w"` (Default: `"1d"`)
* `StartDate` / `EndDate`: `time.Time` (UTC)
* `InitialCapital`: `float64` (e.g., `1000.00`)
* `PeriodicInjection`: `float64` (Cash added to Fiat balance at fixed intervals, e.g., `100.00`)
* `InjectionInterval`: `"daily"` | `"weekly"` | `"biweekly"` | `"monthly"`
* `FeeRate`: `float64` (Taker fee in decimal, default `0.001` for `0.10%`)
* `SlippageRate`: `float64` (Execution penalty in decimal, default `0.0005` for `0.05%`)
* `BaselineType`: `"PURE_DCA"` (invests `PeriodicInjection` immediately upon arrival + `InitialCapital` at $t_0$) or `"LUMP_SUM"` (invests all cash at $t_0$, `PeriodicInjection` = 0).

### 4.2. Order Execution & Friction Math
When the active strategy emits a `BUY` signal with allocation weight $w \in (0, 1]$ at candle close $t$, the order is filled at candle $t+1$ open ($O_{t+1}$):

$$\text{Effective Buy Price } (P_{\text{exec}}) = O_{t+1} \times (1 + \text{SlippageRate})$$

$$\text{Gross Fiat Spent } (F_{\text{spent}}) = \text{CashBalance}_t \times w$$

$$\text{Fee Paid} = F_{\text{spent}} \times \text{FeeRate}$$

$$\text{BTC Acquired } (\Delta \text{BTC}) = \frac{F_{\text{spent}} - \text{Fee Paid}}{P_{\text{exec}}}$$

Conversely, for a `SELL` signal with weight $w \in (0, 1]$ at candle close $t$:

$$\text{Effective Sell Price } (P_{\text{exec}}) = O_{t+1} \times (1 - \text{SlippageRate})$$

$$\text{BTC Sold } (\Delta \text{BTC}) = \text{BTCBalance}_t \times w$$

$$\text{Net Fiat Received} = (\Delta \text{BTC} \times P_{\text{exec}}) \times (1 - \text{FeeRate})$$

### 4.3. Built-In Strategies (v1.0 Roster)
All strategies implement a unified Go interface and operate on pre-calculated indicator slices to ensure $O(1)$ lookup per candle during the simulation loop:

```go
type SignalAction uint8
const (
    ActionHold SignalAction = iota
    ActionBuy
    ActionSell
)

type Signal struct {
    Action SignalAction
    Weight float64 // 0.0 to 1.0 (% of available Cash to spend, or % of BTC to sell)
    Tag    string  // Short label for trade log, e.g., "RSI(14)=24.1 < 30"
}

type Strategy interface {
    ID() string
    Name() string
    WarmupCandles() int
    ComputeIndicators(candles []Candle)
    OnCandle(index int, state PortfolioState) Signal
}
```

1. **Strategy A: Dual Moving Average Crossover (`MA_CROSS`)**
   * *Parameters:* `MAType` (`SMA` | `EMA`), `FastPeriod` (int, default `21`), `SlowPeriod` (int, default `55`), `ExitToFiat` (bool, default `true`).
   * *Logic:* If `FastMA[t-1] <= SlowMA[t-1]` and `FastMA[t] > SlowMA[t]`, emit `ActionBuy` (`Weight: 1.0`). On bearish cross, if `ExitToFiat` is true, emit `ActionSell` (`Weight: 1.0`); otherwise hold existing BTC and simply pause buying with newly injected DCA cash until the next bullish cross.
2. **Strategy B: RSI Dip Accumulator (`RSI_DCA`)**
   * *Parameters:* `RSIPeriod` (int, default `14`), `Oversold` (float, default `30.0`), `Overbought` (float, default `75.0`), `BaseBuyWeight` (float, default `0.25`), `DipBuyWeight` (float, default `1.0`), `TakeProfitWeight` (float, default `0.0`).
   * *Logic:* Periodic cash injections accumulate in the `CashBalance` (Dry Powder). On every injection day, if $30 \le \text{RSI}_t \le 70$, deploy `BaseBuyWeight` of available cash. Whenever $\text{RSI}_t < \text{Oversold}$ (on any candle), deploy `DipBuyWeight` (100% of accumulated Dry Powder). If $\text{RSI}_t > \text{Overbought}$ and `TakeProfitWeight > 0`, sell `TakeProfitWeight` of BTC back into Dry Powder.
3. **Strategy C: MACD Momentum Trend (`MACD_TREND`)**
   * *Parameters:* `FastEMA` (`12`), `SlowEMA` (`26`), `SignalEMA` (`9`), `ZeroLineFilter` (bool).
   * *Logic:* Buy `1.0` when MACD line crosses above Signal line (optionally requiring MACD $< 0$ to catch early reversals). Sell `1.0` (or `0.5`) when MACD crosses below Signal line.
4. **Strategy D: 200-Week SMA Valuation Multiplier (`MACRO_200W`)**
   * *Parameters:* `BaseTrendWeeks` (`200`), `MaxMultiplier` (`3.0`), `OvervaluedRatio` (`2.5`).
   * *Logic:* Computes the ratio $R_t = \text{Close}_t / \text{SMA}_{200W}$. When $R_t \le 1.0$ (price touches or drops below the 200-week moving average), deploy `MaxMultiplier` from the cash reserve. When $R_t > \text{OvervaluedRatio}$, halt purchases and hoard periodic injections in cash.

---

## 5. Benchmark KPIs & Mathematical Formulations

Every simulation produces a `BenchmarkReport` comparing the **Active Strategy** against the **Baseline (Pure DCA or Lump Sum)** over the exact same timestamps and cash injection schedule:

| Metric | Mathematical Definition / Logic | Purpose |
| :--- | :--- | :--- |
| **Final Equity ($/€)** | $E_T = \text{Cash}_T + (\text{BTC}_T \times \text{Close}_T)$ | Raw fiat portfolio valuation at the end of the backtest. |
| **Total Sats Stacked** | $\text{Sats}_T = \lfloor \text{BTC}_T \times 10^8 \rfloor$ | Total Bitcoin accumulated net of all fees. |
| **Sats Alpha ($\alpha_{\text{BTC}}$)** | $\alpha_{\text{BTC}} = \left( \frac{\text{BTC}_{\text{strat}, T} - \text{BTC}_{\text{base}, T}}{\text{BTC}_{\text{base}, T}} \right) \times 100$ | **Primary KPI:** Did active timing accumulate more BTC than brainless DCA? |
| **Time-Weighted Return & IRR** | Annualized Internal Rate of Return (XIRR) accounting for exact timestamps of cash injections. | Prevents periodic cash injections from artificially inflating simple ROI %. |
| **Maximum Drawdown ($\text{MDD}$)** | $\min_{t \in [0, T]} \left( \frac{E_t - \max_{\tau \le t} E_\tau}{\max_{\tau \le t} E_\tau} \right)$ | Measures the worst peak-to-trough psychological pain endured. |
| **Sharpe Ratio** | $\frac{\bar{r}_p - r_f}{\sigma_p} \times \sqrt{N_{\text{periods/year}}}$ (with $r_f = 0$) | Risk-adjusted return penalizing both upside and downside volatility. |
| **Sortino Ratio** | $\frac{\bar{r}_p - r_f}{\sigma_{\text{downside}}} \times \sqrt{N_{\text{periods/year}}}$ | Risk-adjusted return penalizing **only negative** daily/candle returns (crucial for BTC). |
| **Calmar Ratio** | $\frac{\text{Annualized Return}}{\vert{}\text{MDD}\vert{}}$ | Return generated per unit of maximum drawdown risk. |
| **Fee Drag ($/€ & %)** | $\sum_{i=1}^{k} \text{Fee}_i$ and $\frac{\sum \text{Fee}_i}{\text{Total Injected Capital}}$ | Exposes over-trading decay on lower timeframes (`15m`, `1h`). |

---

## 6. TUI Design, Aesthetics & UX Specifications

### 6.1. Color Palette (Catppuccin Mocha + Bitcoin Accent)
Defined in `internal/tui/theme.go` via `lipgloss.Color`:
* **Background / Surface:** `#1E1E2E` (Base), `#313244` (Surface0 for inactive borders)
* **Active Focus Border:** `#CBA6F7` (Mauve, `lipgloss.RoundedBorder()`)
* **Baseline Curve & BTC Accent:** `#F7931A` (Bitcoin Orange)
* **Active Strategy Curve:** `#A6E3A1` (Mint Green if beating baseline) or `#89B4FA` (Sapphire Blue)
* **Drawdown / Sell / Negative Delta:** `#F38BA8` (Soft Red/Pink)
* **Text Hierarchy:** `#CDD6F4` (Primary Text), `#A6ADC8` (Subtext/Labels)

### 6.2. ASCII Wireframe (`pzz-bench` Studio View)

```text
╭─ ₿ pzz-bench v1.0.0 ─────────────────────── [Pair: BTC/USDT] [TF: 1D] [SQLite: Synced] ─╮
│ ╭─ ⚙️ Parameters (Tab) ──╮ ╭─ 📈 Equity Curve: EMA Cross (Green) vs Pure DCA (Orange) ──╮ │
│ │ Strategy  :            │ │ $45k │                                          ⡠⠊⠉⠢⡀      │ │
│ │ > EMA Crossover        │ │ $35k │                                  ⢀⠤⠒⠉⠑⢄ ⡠⠊   ⠈⠢⢀⡴⠋  │ │
│ │                        │ │ $25k │                         ⢀⠤⠒⠉⠑⠢⢄⡠⠊     ⠈⠊            │ │
│ │ Fast EMA  : [ 21   ]   │ │ $15k │          ⣀⠤⠒⠉⠒⠤⡀ ⢀⠤⠒⠉⠑⠢⢄                            │ │
│ │ Slow EMA  : [ 55   ]   │ │  $5k │ ⣀⠤⠒⠉⠒⠤⣀⠤⠊      ⠈⠢⠊                                  │ │
│ │                        │ │      └──────────────────────────────────────────────────── │ │
│ │ Capital   : $1,000     │ │       2022          2023          2024          2025       │ │
│ │ DCA / wk  : $50        │ ╰────────────────────────────────────────────────────────────╯ │
│ │ Fees (%)  : 0.10%      │ ╭─ 📊 MACD (12, 26, 9) & RSI (14) ───────────────────────────╮ │
│ │                        │ │ MACD │   ▄▄▆█▆▄▂       ▂▄▆▄▂         ▂▄▆██▆▄▂              │ │
│ │ [  🚀 Run Benchmark  ] │ │      │ ──███████───▁▃▅██████─────▁▃▅█████████───▁▃▂──────  │ │
│ │                        │ │ RSI  │ ╭╮╭─╮ ╭──╮ ╭───╮  ╭──╮ ╭─╮  ╭───╮   ╭──╮            │ │
│ ╰────────────────────────╯ ╰────────────────────────────────────────────────────────────╯ │
│ ╭─ 🏆 Benchmark Results (Active Strategy vs Baseline DCA) ──────────────────────────────╮ │
│ │ Metric          │ EMA Cross (21/55) │ Baseline (DCA) │ Delta / Alpha │ Sparkline PnL  │ │
│ │─────────────────┼───────────────────┼────────────────┼───────────────┼────────────────│ │
│ │ Final Equity    │ $42,850.20        │ $34,120.00     │ +25.58% 🟢    │ ▁▁▂▂▃▄▃▄▅▆▇██  │ │
│ │ Sats Stacked    │ 61,200,000 sats   │ 48,740,000 sats│ +25.56% ₿     │                │ │
│ │ Max Drawdown    │ -22.4%            │ -54.8%         │ +32.4 pts 🛡️  │                │ │
│ │ Sortino Ratio   │ 2.41              │ 1.38           │ +1.03         │                │ │
│ │ Win Rate (34tx) │ 64.7%             │ N/A (100% Buy) │ Fee: $142.10  │                │ │
│ ╰───────────────────────────────────────────────────────────────────────────────────────╯ │
│  [1/2] Views  [Tab] Focus  [h/l] Pan  [+/-] Zoom  [v] Mode  [r] Run  [e] Export  [?]    │
╰───────────────────────────────────────────────────────────────────────────────────────────╯
```

### 6.3. Responsive Multi-Pane Layout & Modes
The TUI requires a minimum terminal size of `100x30` (gracefully rendering a centered resize warning if smaller) and operates across **2 primary workspaces** switched via `1`, `2`:

#### Workspace `[1]`: Main Benchmark Studio (Default)
* **Left Pane (28 cols fixed) — `huh` Interactive Config:**
  * Scrollable, rounded-border form allowing live editing of Strategy, Timeframe, Date Range, Initial Capital, Periodic DCA, Fee %, and Strategy-specific parameters.
  * Hitting `Enter` on the final field or pressing `r` anywhere triggers an immediate re-computation (takes `<5ms` in Go for 2,500 daily candles) and updates the charts.
* **Top-Right Pane (Dynamic Width, 55% Height) — `ntcharts` Dual Equity / Price Canvas:**
  * Uses `timeserieslinechart` with line-run rendering (`╭╮╰╯─│`, one cell per point segment).
  * **Toggleable Chart Modes (`v` key):**
    1. *Fiat Equity Curve ($/€):* Strategy Total Value vs. Baseline DCA Total Value vs. Total Cash Injected.
    2. *Sats Accumulation Curve (sats):* Total BTC/sats held by Strategy vs. Baseline DCA over time.
    3. *Underwater Drawdown (%):* Plots the percentage drawdown from all-time highs for both curves (`0%` at the top, dipping down to `-70%`).
* **Middle-Right Pane (Dynamic Width, 20% Height) — Secondary Oscillator Canvas:**
  * Synchronized X-axis with the main chart. Displays either the active indicator (e.g., RSI with `30`/`70` horizontal dashed lines, or MACD histogram using Unicode vertical block elements `▁▂▃▄▅▆▇█`).
* **Bottom Pane (Full Width, 25% Height) — Comparative KPI & Trade Log Table:**
  * Sub-tabs (`[` and `]`): **KPI Summary Table** (side-by-side Strategy vs. Baseline with colored Delta pills) and **Execution Ledger** (scrollable table of every `BUY`/`SELL` trade, timestamp, price, fee paid, and trigger tag).

#### Workspace `[2]`: Tools (Optimizer + Market Cache)
* The main pane has two sub-tabs toggled with `[` / `]`:
  * **2D Parameter Grid Optimizer** — triggered via `g` or on first entry. Spawns a pool of `runtime.NumCPU()` goroutines to run hundreds of backtest permutations concurrently (e.g. `FastEMA` from `5` to `60` step `5` on the X-axis against `SlowEMA` from `40` to `240` step `20` on the Y-axis). Renders an ANSI TrueColor heatmap matrix where each cell displays the **Sats Alpha ($\alpha_{\text{BTC}}$)** or **Sortino Ratio** for that coordinate pair.
  * **Market Cache** — displays SQLite database status per timeframe (`BTCUSDT 15m`, `1h`, `4h`, `1d`, `1w`), total candle count, earliest/latest timestamps and disk usage. Press `u` to sync historical archives from Binance/Mempool.

### 6.4. Complete Keyboard Navigation Matrix
| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Tab` / `Shift+Tab` | Global | Cycle focus: sidebar → chart → results (studio) or sidebar → main (tools) |
| `1` / `2` | Global | Switch between `[1] Studio` and `[2] Tools` (outside the sidebar) |
| `r` / `Ctrl+R` | Global | Execute / Re-run benchmark with current parameters |
| `v` | Chart Focus | Cycle chart mode: `Fiat Equity ($)` $\rightarrow$ `Sats Stack (sats)` $\rightarrow$ `Drawdown (%)` |
| `h` / `l` or `←` / `→` | Chart Focus | Pan time window left / right |
| `+` / `-` or `k` / `j` | Chart Focus | Zoom time window in / out |
| `0` | Chart Focus | Reset zoom to 100% full backtest range |
| `[` / `]` | Studio results | Toggle between `KPI Comparison` and `Trade Execution Ledger` |
| `↑` / `↓` / `PgUp` / `PgDn` / `Home` / `End` | Results table | Row navigation in the focused table |
| `[` / `]` | Tools | Switch between the optimizer and the market cache |
| `g` | Tools · optimizer | Re-run the 2D parameter grid |
| `m` | Tools · optimizer | Cycle heatmap metric: Sats Alpha → Sortino → Equity Alpha |
| `←` `↑` `↓` `→` | Tools · optimizer | Navigate heatmap cells |
| `u` | Tools · cache | Pull missing candles from Binance/Mempool |
| `s` | Global | Save current parameter configuration to `~/.config/pzz-bench/presets.toml` |
| `e` | Global | Export full benchmark report + Markdown table + CSV trade ledger to `./exports/` |
| `?` | Global | Toggle the keybinding overlay |
| `q` / `Ctrl+C` | Global | Graceful exit (flushes SQLite WAL and restores terminal state) |

---

## 7. Non-Functional Requirements & Performance Targets

1. **Simulation Latency:** Running a single backtest over 10,000 candles (approx. 14 months of `1h` data or 27 years of `1d` data) including indicator calculation and KPI aggregation **must complete in `< 15ms`** on a standard modern CPU, ensuring 60 FPS UI responsiveness.
2. **Grid Search Throughput:** Executing a `10x10` parameter grid (100 full backtests) in Workspace `[2]` must complete in **`< 250ms`** by pre-allocating slices and sharing the immutable `[]Candle` array across worker goroutines without mutex contention.
3. **Offline Resilience:** Once a timeframe is synced in SQLite, `pzz-bench` must function 100% offline with zero network calls unless the user explicitly presses `u` (Update Cache) or requests a date range beyond the cached ceiling.
4. **Rate-Limit Safety:** The Binance downloader must batch requests (1,000 candles per HTTP GET) with a polite `150ms` delay and automatic exponential backoff if HTTP `429` is encountered.

---

## 8. Implementation Milestones (Engineering Execution Plan)

* **Milestone 1: Storage & Data Ingestion (`internal/data`)**
  * Implement `modernc.org/sqlite` store, Binance `/api/v3/klines` paginated downloader, and Mempool.space historical daily merger.
* **Milestone 2: Deterministic Engine & Indicators (`internal/engine`)**
  * Implement vectorized SMA, EMA, RSI, MACD, and 200W SMA calculators.
  * Build the `Simulator` loop (enforcing `Close[t]` signal $\rightarrow$ `Open[t+1]` execution), `PortfolioState` accounting, and `BenchmarkReport` math (XIRR, Sortino, MDD, Sats Alpha).
  * Write unit tests verifying zero look-ahead bias and exact fee deduction.
* **Milestone 3: Charm TUI Shell & Form Binding (`internal/tui`)**
  * Scaffold the `bubbletea` root model, responsive `lipgloss` layout, and `huh` sidebar form wired to trigger asynchronous `tea.Cmd` backtest runs.
* **Milestone 4: Line-Run Charting & Grid Optimizer (`internal/tui/components`)**
  * Integrate `ntcharts` dual-series line-run charts, block-character MACD histograms, and the concurrent 2D heatmap optimizer view.