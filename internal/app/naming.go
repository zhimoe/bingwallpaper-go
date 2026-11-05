package app

import (
	"fmt"
	"path/filepath"

	"github.com/zhimoe/bingwallpaper/internal/bing"
	"github.com/zhimoe/bingwallpaper/internal/config"
)

// outputName resolves the local path for an asset. Without --keep-file-name
// every asset kind gets a stable, predictable name so collectors never
// overwrite each other.
func outputName(cfg config.Config, asset bing.Asset, index int) string {
	name := bing.FileName(asset.URL)
	if !cfg.KeepFileName {
		ext := filepath.Ext(name)
		if ext == "" {
			ext = ".jpg"
		}
		switch asset.Kind {
		case bing.AssetMainImage:
			name = "wallpaper" + ext
		case bing.AssetAccompanyImage:
			name = "wallpaper-accompany" + ext
		default:
			name = fmt.Sprintf("wallpaper-asset-%d%s", index, ext)
		}
	}
	return filepath.Join(cfg.OutputFolder, name)
}
