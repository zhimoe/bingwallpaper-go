//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartupCommandUsesWindowsEscaping(t *testing.T) {
	command := startupCommand(`C:\Program Files\Bing Wallpaper\BingWallpaper.exe`, `C:\Users\Test User\settings.conf`, "background")
	if strings.Contains(command, `C:\\Program`) {
		t.Fatalf("command contains Go-style escaped slashes: %s", command)
	}
	if !strings.Contains(command, `"C:\Program Files\Bing Wallpaper\BingWallpaper.exe"`) || !strings.HasSuffix(command, "--background") {
		t.Fatalf("command = %s", command)
	}
}

func TestAcquireSingleInstance(t *testing.T) {
	name := fmt.Sprintf(`Local\bingwallpaper-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	release, err := AcquireSingleInstance(name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	secondRelease, err := AcquireSingleInstance(name)
	if secondRelease != nil {
		secondRelease()
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire error = %v", err)
	}
}

func TestPreferredStartupExecutableUsesGUISibling(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "BingWallpaper-cli.exe")
	gui := filepath.Join(dir, "BingWallpaper.exe")
	if err := os.WriteFile(gui, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := preferredStartupExecutable(cli); got != gui {
		t.Fatalf("preferred executable = %s, want %s", got, gui)
	}
}
