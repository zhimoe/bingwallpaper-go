//go:build windows

package platform

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/zhimoe/bingwallpaper/internal/fsutil"
)

const (
	spiSetDeskWallpaper = 0x0014
	// Only persist the new wallpaper. Do not add SPIF_SENDCHANGE (0x02):
	// it broadcasts WM_SETTINGCHANGE, which makes Explorer and the taskbar
	// repaint/flicker on every set.
	spifUpdateINIFile = 0x01
	desktopKey        = `Control Panel\Desktop`
	runKey            = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupValue      = "GoBingWallpaper"
)

var procSystemParametersInfoW = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")

type windowsWallpaperSetter struct{}

func newWallpaperSetter() WallpaperSetter { return windowsWallpaperSetter{} }

func (windowsWallpaperSetter) Set(inputPath string) error {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return err
	}
	bmpPath := filepath.Join(filepath.Dir(abs), "wallpaper.bmp")
	bmpChanged := false
	switch {
	case strings.EqualFold(filepath.Ext(abs), ".bmp"):
		bmpPath = abs
	case needsConversion(abs, bmpPath):
		if err := convertToBMP(abs, bmpPath); err != nil {
			return err
		}
		bmpChanged = true
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开桌面注册表: %w", err)
	}
	defer key.Close()

	// Read failures are ignored: a missing/wrong-typed value is treated as
	// "not configured yet" and repaired below.
	current, _, _ := key.GetStringValue("Wallpaper")
	tile, _, _ := key.GetStringValue("TileWallpaper")
	style, _, _ := key.GetStringValue("WallpaperStyle")
	if canSkipSet(current, bmpPath, tile, style, bmpChanged) {
		return nil
	}

	if _, _, err := key.GetStringValue("WallpaperBackup"); errors.Is(err, registry.ErrNotExist) {
		if current != "" && !samePath(current, bmpPath) {
			_ = key.SetStringValue("WallpaperBackup", current)
		}
	}
	if tile != "0" {
		if err := key.SetStringValue("TileWallpaper", "0"); err != nil {
			return err
		}
	}
	if style != "10" {
		if err := key.SetStringValue("WallpaperStyle", "10"); err != nil {
			return err
		}
	}

	ptr, err := windows.UTF16PtrFromString(bmpPath)
	if err != nil {
		return err
	}
	r1, _, callErr := procSystemParametersInfoW.Call(spiSetDeskWallpaper, 0, uintptr(unsafe.Pointer(ptr)), spifUpdateINIFile)
	if r1 == 0 {
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("SystemParametersInfoW: %w", callErr)
		}
		return errors.New("SystemParametersInfoW 返回失败")
	}
	// SPI_SETDESKWALLPAPER normally updates this value itself, but writing it
	// explicitly makes the fast path above reliable across Windows versions.
	if !samePath(current, bmpPath) {
		if err := key.SetStringValue("Wallpaper", bmpPath); err != nil {
			slog.Warn("写入壁纸注册表失败", "error", err)
		}
	}
	return nil
}

// canSkipSet and needsConversion live in wallpaper_cache.go so their logic is
// testable on every platform.

func convertToBMP(input, output string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("解码壁纸: %w", err)
	}
	out, err := os.CreateTemp(filepath.Dir(output), ".wallpaper-*.bmp")
	if err != nil {
		return err
	}
	tempName := out.Name()
	defer os.Remove(tempName)
	if err := bmp.Encode(out, img); err != nil {
		out.Close()
		return fmt.Errorf("转换 BMP: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return fsutil.ReplaceFile(tempName, output)
}

type windowsStartupManager struct{}

func newStartupManager() StartupManager { return windowsStartupManager{} }

func (windowsStartupManager) Set(executable, mode, configFile string) error {
	abs, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	abs = preferredStartupExecutable(abs)
	configFile, err = filepath.Abs(configFile)
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开自启动注册表: %w", err)
	}
	defer key.Close()
	return key.SetStringValue(startupValue, startupCommand(abs, configFile, mode))
}

func preferredStartupExecutable(executable string) string {
	if !strings.EqualFold(filepath.Base(executable), "BingWallpaper-cli.exe") {
		return executable
	}
	candidate := filepath.Join(filepath.Dir(executable), "BingWallpaper.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return executable
}

func startupCommand(executable, configFile, mode string) string {
	arg := "--foreground"
	if mode == "background" {
		arg = "--background"
	}
	return windows.ComposeCommandLine([]string{executable, "--config-file", configFile, arg})
}

func (windowsStartupManager) Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(startupValue)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func (windowsStartupManager) Status() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "disabled", nil
	}
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(startupValue)
	if errors.Is(err, registry.ErrNotExist) {
		return "disabled", nil
	}
	if err != nil {
		return "", err
	}
	if strings.Contains(value, "--background") {
		return "background: " + value, nil
	}
	return "foreground: " + value, nil
}

func acquireSingleInstance(name string) (func(), error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, true, namePtr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, fmt.Errorf("创建单实例互斥量: %w", err)
	}
	return func() {
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
	}, nil
}
