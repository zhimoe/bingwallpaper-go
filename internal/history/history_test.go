package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPythonCompatibleHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "wallpaper.jpg")
	if err := os.WriteFile(image, []byte("jpg"), 0o600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "history.json")
	rec := NewRecord("https://example/w.jpg", image, "desc", "zh-CN", time.Now(), time.Now(), false, []byte("not serialized"))
	s := New()
	s.Replace(rec)
	if err := s.Save(filename); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.Get(rec.URL)
	if !ok || got.LocalFile != image || got.Raw != nil {
		t.Fatalf("record = %#v, ok=%t", got, ok)
	}
}

func TestLoadDropsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "history.json")
	if err := os.WriteFile(filename, []byte(`{"u":{"url":"u","local_file":"missing.jpg","description":"d","time":"t","start_time":null,"end_time":null,"is_accompany":false,"market":""}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("u"); ok {
		t.Fatal("missing local file should be discarded")
	}
}

func TestReplaceInvalidatesOverwrittenFile(t *testing.T) {
	s := New()
	s.Replace(Record{URL: "old", LocalFile: "wallpaper.jpg"})
	s.Replace(Record{URL: "retained", LocalFile: "other.jpg"})
	s.Replace(Record{URL: "new", LocalFile: "wallpaper.jpg", Raw: []byte("large image")})
	if _, ok := s.Get("old"); ok {
		t.Fatal("overwritten image must not remain cached")
	}
	if _, ok := s.Get("retained"); !ok {
		t.Fatal("unrelated image should remain cached")
	}
	if rec, ok := s.Get("new"); !ok || rec.Raw != nil {
		t.Fatalf("new record = %#v, ok = %t", rec, ok)
	}
}

func TestLoadNullHistory(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(filename, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	s.Replace(Record{URL: "new", LocalFile: "wallpaper.jpg"})
	if _, ok := s.Get("new"); !ok {
		t.Fatal("null history must support new records")
	}
}

func TestLoadDropsEmptyFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.jpg")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Replace(Record{URL: "empty", LocalFile: empty})
	s.Replace(Record{URL: "directory", LocalFile: dir})
	filename := filepath.Join(dir, "history.json")
	if err := s.Save(filename); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.records) != 0 {
		t.Fatalf("invalid cached files: %v", loaded.records)
	}
}
