// Package config resolves XDG-compliant paths and persists TOML presets.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Paths bundles every user-level location pzz-bench touches.
type Paths struct {
	ConfigDir string // XDG config dir (~/.config/pzz-bench)
	DataDir   string // XDG data dir (~/.local/share/pzz-bench)
	ExportsDir  string // ./exports relative to the current working directory
}

// DatabasePath is market.db inside DataDir.
func (p Paths) DatabasePath() string { return filepath.Join(p.DataDir, "market.db") }

// PresetsFile is presets.toml inside ConfigDir.
func (p Paths) PresetsFile() string { return filepath.Join(p.ConfigDir, "presets.toml") }

// Resolve computes the paths honoring XDG_CONFIG_HOME / XDG_DATA_HOME when
// set, falling back to platform conventions (native Windows: %APPDATA% and
// %LOCALAPPDATA%).
func Resolve() Paths {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = defaultConfigDir()
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = defaultDataDir()
	}
	return Paths{
		ConfigDir:  filepath.Join(cfg, "pzz-bench"),
		DataDir:    filepath.Join(data, "pzz-bench"),
		ExportsDir: "exports",
	}
}

func defaultConfigDir() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return appdata
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return local
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share")
}

// EnsureDirs creates ConfigDir and DataDir (and their parents).
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.ConfigDir, p.DataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	return nil
}

// Preset is one saved configuration snapshot.
type Preset struct {
	Name              string   `toml:"-"`
	Symbol            string   `toml:"symbol"`
	Timeframe         string   `toml:"timeframe"`
	Start             string   `toml:"start"`
	End               string   `toml:"end"`
	InitialCapital    float64  `toml:"initial_capital"`
	PeriodicInjection float64  `toml:"periodic_injection"`
	InjectionInterval string   `toml:"injection_interval"`
	FeeRate           float64  `toml:"fee_rate"`
	SlippageRate      float64  `toml:"slippage_rate"`
	Baseline          string   `toml:"baseline"`
	Strategy          string   `toml:"strategy"`
	StrategyParams    map[string]string `toml:"strategy_params"`
}

// presetsFile mirrors presets.toml.
type presetsFile struct {
	Presets map[string]Preset `toml:"presets"`
}

// LoadPresets reads every saved preset (missing file => empty, no error).
func (p Paths) LoadPresets() (map[string]Preset, error) {
	raw, err := os.ReadFile(p.PresetsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Preset{}, nil
		}
		return nil, err
	}
	var f presetsFile
	if err := toml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p.PresetsFile(), err)
	}
	return f.Presets, nil
}

// SavePreset writes (or overwrites) one preset, keeping the others intact.
func (p Paths) SavePreset(preset Preset) error {
	if preset.Name == "" {
		preset.Name = "last-run"
	}
	existing, err := p.LoadPresets()
	if err != nil {
		existing = map[string]Preset{}
	}
	if existing == nil {
		existing = map[string]Preset{}
	}
	existing[preset.Name] = preset
	if err := os.MkdirAll(p.ConfigDir, 0o755); err != nil {
		return err
	}
	f := presetsFile{Presets: existing}
	out, err := toml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(p.PresetsFile(), out, 0o644)
}

// ParseDate parses "2006-01-02" (or full RFC3339) into a UTC time.Time.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q (use YYYY-MM-DD)", s)
}

