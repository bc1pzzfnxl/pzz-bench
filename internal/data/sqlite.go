package data

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bc1pzzfnxl/pzz-bench/internal/engine"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (zero CGo)
)

// Store is the SQLite-backed candle cache for market data.
type Store struct {
	DB *sql.DB
}

const schema = `
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
`

// OpenStore opens (creating if needed) the market.db cache.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(0)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite likes a single writer
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}
	return &Store{DB: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.DB.Close() }

// UpsertCandles INSERT OR REPLACE batch candles in a single transaction.
func (s *Store) UpsertCandles(symbol, timeframe string, candles []engine.Candle) error {
	if len(candles) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO candles
		(symbol, timeframe, open_time, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range candles {
		if _, err := stmt.Exec(symbol, timeframe, c.OpenTime, c.Open, c.High, c.Low, c.Close, c.Volume); err != nil {
			return fmt.Errorf("upserting candle %s/%s@%d: %w", symbol, timeframe, c.OpenTime, err)
		}
	}
	return tx.Commit()
}

// Extents returns (earliest, latest, count) for one symbol/timeframe, or
// (0,0,0) when nothing is cached. count>0 always implies min/max valid.
func (s *Store) Extents(symbol, timeframe string) (minMs, maxMs int64, count int64, err error) {
	row := s.DB.QueryRow(`SELECT COALESCE(MIN(open_time),0), COALESCE(MAX(open_time),0), COUNT(*)
		FROM candles WHERE symbol=? AND timeframe=?`, symbol, timeframe)
	if err := row.Scan(&minMs, &maxMs, &count); err != nil {
		return 0, 0, 0, err
	}
	if count == 0 {
		return 0, 0, 0, nil
	}
	return minMs, maxMs, count, nil
}

// LoadRange returns candles with OpenTime in [startMs, endMs] ascending.
func (s *Store) LoadRange(symbol, timeframe string, startMs, endMs int64) ([]engine.Candle, error) {
	rows, err := s.DB.Query(`SELECT open_time, open, high, low, close, volume
		FROM candles WHERE symbol=? AND timeframe=? AND open_time BETWEEN ? AND ?
		ORDER BY open_time ASC`, symbol, timeframe, startMs, endMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Candle
	for rows.Next() {
		var c engine.Candle
		if err := rows.Scan(&c.OpenTime, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		c.CloseTime = c.OpenTime + engine.TimeframeMS(timeframe) - 1
		out = append(out, c)
	}
	return out, rows.Err()
}
