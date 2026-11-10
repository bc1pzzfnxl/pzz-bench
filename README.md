# pzz-bench

Bitcoin accumulation studio: a zero look-ahead backtesting TUI. Compares any
strategy against a DCA / lump-sum baseline with KPIs, trade ledger and a 2D
parameter-grid optimizer.

## Build

```sh
go build ./...
go vet ./...
go test ./...
```

Single static binary (pure Go, no CGo):

```sh
go build -o pzz-bench ./cmd/pzz-bench
```

## Run

```sh
./pzz-bench
./pzz-bench --wizard
./pzz-bench --version
./pzz-bench --symbol BTCUSDT --timeframe 1d --start 2020-01-01 --end 2026-01-01 \
  --capital 1000 --dca 50 --interval weekly --fee 0.10 --slippage 0.05 \
  --baseline PURE_DCA --strategy MA_CROSS --param fast=50,slow=200
```

`--wizard` skips the TUI and walks through the same parameters in two steps:
the shared window/costs form first, then the selected strategy's fields. It
prints the resolved strategy parameters, runs the benchmark and writes the
report and trades CSV to `./exports`.

Data is cached in SQLite (`market.db` under the XDG data dir) and filled on
demand from Binance (BTCUSDT, BTCEUR) and mempool.space (BTCUSD, pre-2017
history).

## Workspaces

- **Studio** (`1`): parameter sidebar, line-run equity/sats/drawdown chart,
  oscillator, and the KPI / trade-ledger tables.
- **Tools** (`2`): `[`/`]` switch between the 2D parameter-grid optimizer and
  the market-cache inventory.

The title bar shows the latest status (errors in red); the footer only lists
keys that the focused zone handles.

## Keys

- `tab` / `shift+tab` cycle focus: sidebar → chart → results (studio) or
  sidebar → main (tools)
- `1`/`2` switch workspaces outside the sidebar
- `r` run benchmark, `s` save preset, `e` export report + trades CSV
- `v` cycle chart mode (fiat equity → sats → drawdown)
- `h`/`l` pan, `+`/`-` zoom, `0` reset
- `[`/`]` cycle result tabs in the studio, optimizer/cache sub-tabs in tools
- `↑`/`↓` rows, `PgUp`/`PgDn` page, `Home`/`End` jump in the focused table
- `g` run grid, `m` cycle grid metric, arrows navigate heatmap cells
- `u` sync cache from the providers
- `?` help, `q` quit

## Baseline semantics

- `PURE_DCA`: the initial capital is invested at t0 and every periodic
  injection is invested on arrival for both the strategy and the baseline.
- `LUMP_SUM`: the initial capital is invested once at t0 for both curves;
  periodic injections are disabled (DCA fields are hidden in the sidebar).

## Periods

MA / MACD / RSI periods are counts of the selected timeframe's candles, not
calendar days. `21/55` defaults assume a daily timeframe; on `1w` they span
~5 months / ~1 year. The sidebar shows the active timeframe and labels these
fields `(candles)`.

## Known limitations

- Drawdown is plotted as a positive depth with negative axis tick labels.
- Money formatting hardcodes `$` (BTCEUR is cosmetic only).
- mempool.space EUR series only exists from ~2015; earlier weeks are skipped.
- BTCUSD is mempool-only (daily/weekly granularity).
- XIRR uses a 365-day year via widening bisection.
PY