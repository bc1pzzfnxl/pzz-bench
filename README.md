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
./pzz-bench --version
./pzz-bench --symbol BTCUSDT --timeframe 1d --start 2020-01-01 --end 2026-01-01 \
  --capital 1000 --dca 50 --interval weekly --fee 0.10 --slippage 0.05 \
  --baseline PURE_DCA --strategy MA_CROSS --param fast=50,slow=200
```

Data is cached in SQLite (`market.db` under the XDG data dir) and filled on
demand from Binance (BTCUSDT, BTCEUR) and mempool.space (BTCUSD, pre-2017
history).

## Keys (PRD 6.4)

- `tab` / `shift+tab` cycle focus: sidebar → chart → bottom
- `1`/`2`/`3` switch workspaces (studio / grid / data) outside the sidebar
- `r` run benchmark, `g` run grid, `m` cycle grid metric, `u` sync cache
- `v` cycle chart mode (fiat equity → sats → drawdown)
- `h`/`l` pan, `+`/`-` zoom, `0` reset
- `[`/`]` cycle bottom tabs, `j`/`k` rows, `t`/`b` top/bottom
- `s` save preset, `e` export report + trades CSV, `?` help, `q` quit

## Known limitations

- Drawdown is plotted as a positive depth with negative axis tick labels.
- Money formatting hardcodes `$` (BTCEUR is cosmetic only).
- mempool.space EUR series only exists from ~2015; earlier weeks are skipped.
- BTCUSD is mempool-only (daily/weekly granularity).
- XIRR uses a 365-day year via widening bisection.
PY