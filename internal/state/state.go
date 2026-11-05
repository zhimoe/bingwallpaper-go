// Package state records the wallpaper that was most recently applied so the
// daemon can avoid re-setting the same image more than once per day.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/fsutil"
)

// State is persisted in a small JSON file outside the Python-compatible
// download history.
type State struct {
	LastAppliedURL  string    `json:"last_applied_url"`
	LastAppliedFile string    `json:"last_applied_file"`
	LastAppliedAt   time.Time `json:"last_applied_at"`
}

// Load reads the state file. A missing file yields a zero State and no error.
func Load(filename string) (State, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// Save atomically writes the state file.
func Save(filename string, s State) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return fsutil.ReplaceFile(name, filename)
}

// AppliedToday reports whether url was applied from file on the same local
// calendar day as now.
func (s State) AppliedToday(url, file string, now time.Time) bool {
	return url != "" &&
		s.LastAppliedURL == url &&
		s.LastAppliedFile == file &&
		SameLocalDay(s.LastAppliedAt, now)
}

// SameLocalDay compares two instants by calendar date in the local timezone.
func SameLocalDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}
