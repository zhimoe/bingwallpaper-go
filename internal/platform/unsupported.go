//go:build !windows

package platform

import "errors"

var errWindowsOnly = errors.New("此功能仅支持 Windows")

type unsupportedWallpaper struct{}
type unsupportedStartup struct{}

func newWallpaperSetter() WallpaperSetter                   { return unsupportedWallpaper{} }
func newStartupManager() StartupManager                     { return unsupportedStartup{} }
func (unsupportedWallpaper) Set(string) error               { return errWindowsOnly }
func (unsupportedStartup) Set(string, string, string) error { return errWindowsOnly }
func (unsupportedStartup) Disable() error                   { return errWindowsOnly }
func (unsupportedStartup) Status() (string, error)          { return "unsupported", errWindowsOnly }

func acquireSingleInstance(string) (func(), error) { return func() {}, nil }
