package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// canSkipSet avoids the SPI call when the same image is already the active
// wallpaper, the desktop style is correct, and the BMP was not just rebuilt.
func canSkipSet(current, bmpPath, tile, style string, bmpChanged bool) bool {
	return !bmpChanged && samePath(current, bmpPath) && tile == "0" && style == "10"
}

// samePath compares Windows paths case-insensitively.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// needsConversion reports whether the cached BMP is older than its source.
// The download path atomically replaces the source, so mtime is a reliable
// signal here even though it cannot prove that a wallpaper was applied.
func needsConversion(source, output string) bool {
	src, err := os.Stat(source)
	if err != nil {
		return true
	}
	dst, err := os.Stat(output)
	if err != nil || !dst.Mode().IsRegular() || dst.Size() == 0 {
		return true
	}
	// Require the BMP to be strictly newer. Equal timestamps (common on
	// filesystems with coarse time resolution) trigger a safe rebuild.
	return !dst.ModTime().After(src.ModTime())
}
