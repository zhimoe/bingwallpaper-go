package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingReturnsZero(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s != (State{}) {
		t.Fatalf("state = %#v", s)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "state.json")
	want := State{
		LastAppliedURL:  "https://bing.test/image.jpg",
		LastAppliedFile: `C:\Wallpapers\wallpaper.jpg`,
		LastAppliedAt:   time.Date(2026, 11, 5, 10, 42, 0, 0, time.Local),
	}
	if err := Save(filename, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastAppliedURL != want.LastAppliedURL ||
		got.LastAppliedFile != want.LastAppliedFile ||
		!got.LastAppliedAt.Equal(want.LastAppliedAt) {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
}

func TestSaveReplacesCorruptFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(filename, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filename); err == nil {
		t.Fatal("expected corrupt state error")
	}
	want := State{LastAppliedURL: "u", LastAppliedFile: "f", LastAppliedAt: time.Now()}
	if err := Save(filename, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastAppliedURL != "u" {
		t.Fatalf("state = %#v", got)
	}
}

func TestAppliedToday(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	s := State{
		LastAppliedURL:  "https://bing.test/today.jpg",
		LastAppliedFile: `C:\Wallpapers\wallpaper.jpg`,
		LastAppliedAt:   now.Add(-2 * time.Hour),
	}
	if !s.AppliedToday("https://bing.test/today.jpg", `C:\Wallpapers\wallpaper.jpg`, now) {
		t.Fatal("same URL/file/day should be treated as applied today")
	}
	if s.AppliedToday("https://bing.test/other.jpg", `C:\Wallpapers\wallpaper.jpg`, now) {
		t.Fatal("different URL must not be skipped")
	}
	if s.AppliedToday("https://bing.test/today.jpg", `C:\Wallpapers\other.jpg`, now) {
		t.Fatal("different file must not be skipped")
	}
	if s.AppliedToday("https://bing.test/today.jpg", `C:\Wallpapers\wallpaper.jpg`, now.AddDate(0, 0, 1)) {
		t.Fatal("yesterday's state must not be treated as today")
	}
}

func TestSameLocalDayHandlesZeroAndTimezones(t *testing.T) {
	zero := time.Time{}
	if SameLocalDay(zero, time.Now()) || SameLocalDay(time.Now(), zero) {
		t.Fatal("zero time must not match")
	}
	now := time.Date(2026, 11, 5, 23, 30, 0, 0, time.Local)
	if !SameLocalDay(now, now.Add(20*time.Minute)) {
		t.Fatal("same local calendar day should match")
	}
	if SameLocalDay(now, now.Add(24*time.Hour)) {
		t.Fatal("next day must not match")
	}
}
