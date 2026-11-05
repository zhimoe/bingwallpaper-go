package history

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/fsutil"
)

// Record is compatible with both the legacy JSON history and SQLite schema.
type Record struct {
	URL         string  `json:"url"`
	LocalFile   string  `json:"local_file"`
	Description string  `json:"description"`
	Time        string  `json:"time"`
	StartTime   *string `json:"start_time"`
	EndTime     *string `json:"end_time"`
	IsAccompany bool    `json:"is_accompany"`
	Market      string  `json:"market"`
	Raw         []byte  `json:"-"`
}

// Store keeps the small URL-indexed history used for duplicate detection.
type Store struct{ records map[string]Record }

// New returns an empty history store.
func New() *Store { return &Store{records: make(map[string]Record)} }

// Load reads history and discards entries without a nonempty regular file.
func Load(filename string) (*Store, error) {
	s := New()
	f, err := os.Open(filename)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&s.records); err != nil {
		return nil, err
	}
	if s.records == nil {
		s.records = make(map[string]Record)
	}
	for key, rec := range s.records {
		if info, err := os.Stat(rec.LocalFile); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			delete(s.records, key)
		}
	}
	return s, nil
}

// Get looks up a record by its remote URL.
func (s *Store) Get(rawURL string) (Record, bool) { r, ok := s.records[rawURL]; return r, ok }

// Replace upserts rec and invalidates older URLs sharing its output path.
// Records for other paths remain available for duplicate detection.
func (s *Store) Replace(rec Record) {
	// A fixed output filename may now contain a different day's image.
	maps.DeleteFunc(s.records, func(key string, old Record) bool {
		return key != rec.URL && old.LocalFile == rec.LocalFile
	})
	// Raw bytes belong in SQLite only; the history needs metadata alone.
	rec.Raw = nil
	if s.records == nil {
		s.records = make(map[string]Record)
	}
	s.records[rec.URL] = rec
}

// Save atomically writes the history as JSON.
func (s *Store) Save(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".history-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err = enc.Encode(s.records); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return fsutil.ReplaceFile(name, filename)
}

// NewRecord builds a record using the legacy timestamp representation.
func NewRecord(rawURL, localFile, description, market string, start, end time.Time, accompany bool, raw []byte) Record {
	r := Record{URL: rawURL, LocalFile: localFile, Description: description, Time: time.Now().UTC().Format("2006-01-02T15:04:05.999999"), IsAccompany: accompany, Market: market, Raw: raw}
	if start.IsZero() {
		r.StartTime = nil
	} else {
		value := start.Format("2006-01-02T15:04:05")
		r.StartTime = &value
	}
	if end.IsZero() {
		r.EndTime = nil
	} else {
		value := end.Format("2006-01-02")
		r.EndTime = &value
	}
	return r
}
