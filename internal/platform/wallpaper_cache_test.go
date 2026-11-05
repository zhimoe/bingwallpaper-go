package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanSkipSet(t *testing.T) {
	bmp := `C:\Wallpapers\wallpaper.bmp`
	if !canSkipSet(`c:\wallpapers\WALLPAPER.BMP`, bmp, "0", "10", false) {
		t.Fatal("same wallpaper with unchanged BMP and correct style should be skipped")
	}
	for _, tt := range []struct {
		name       string
		current    string
		tile       string
		style      string
		bmpChanged bool
	}{
		{"different wallpaper", `C:\other.bmp`, "0", "10", false},
		{"empty wallpaper", "", "0", "10", false},
		{"wrong tile", bmp, "1", "10", false},
		{"wrong style", bmp, "0", "0", false},
		{"bmp rebuilt", bmp, "0", "10", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if canSkipSet(tt.current, bmp, tt.tile, tt.style, tt.bmpChanged) {
				t.Fatal("expected a real set")
			}
		})
	}
}

func TestNeedsConversion(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "wallpaper.jpg")
	output := filepath.Join(dir, "wallpaper.bmp")
	if err := os.WriteFile(source, []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !needsConversion(source, output) {
		t.Fatal("missing output should need conversion")
	}
	if err := os.WriteFile(output, []byte("bmp"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	newer := time.Now()
	if err := os.Chtimes(output, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	if needsConversion(source, output) {
		t.Fatal("fresh output should not be converted again")
	}
	if err := os.Chtimes(source, newer.Add(time.Minute), newer.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !needsConversion(source, output) {
		t.Fatal("newer source should trigger conversion")
	}
	if err := os.WriteFile(output, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !needsConversion(source, output) {
		t.Fatal("empty output should trigger conversion")
	}
}
