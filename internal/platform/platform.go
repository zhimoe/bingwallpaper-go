package platform

import "errors"

// ErrAlreadyRunning is returned when another instance owns the process mutex.
var ErrAlreadyRunning = errors.New("程序已经在运行")

// WallpaperSetter applies a local image to the current user's desktop.
type WallpaperSetter interface{ Set(path string) error }

// StartupManager manages the current user's login startup registry value.
type StartupManager interface {
	Set(executable, mode, configFile string) error
	Disable() error
	Status() (string, error)
}

// NewWallpaperSetter returns the platform wallpaper implementation.
func NewWallpaperSetter() WallpaperSetter { return newWallpaperSetter() }

// NewStartupManager returns the platform startup implementation.
func NewStartupManager() StartupManager { return newStartupManager() }

// AcquireSingleInstance acquires a process-wide named lock.
func AcquireSingleInstance(name string) (release func(), err error) {
	return acquireSingleInstance(name)
}
