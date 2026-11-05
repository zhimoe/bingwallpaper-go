package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/archive"
	"github.com/zhimoe/bingwallpaper/internal/bing"
	"github.com/zhimoe/bingwallpaper/internal/config"
	"github.com/zhimoe/bingwallpaper/internal/download"
	"github.com/zhimoe/bingwallpaper/internal/history"
	"github.com/zhimoe/bingwallpaper/internal/platform"
	appstate "github.com/zhimoe/bingwallpaper/internal/state"
)

var fileLogging = "false"

// guiBuild is set to "true" at link time for the windowless build; only that
// build registers startup automatically on launch.
var guiBuild = "false"

var (
	logWriterOnce sync.Once
	logWriter     io.Writer = os.Stderr
)

type recordStore interface {
	Get(string) (history.Record, bool)
	Replace(history.Record)
	Save(string) error
}

type wallpaperSetter interface {
	Set(string) error
}

type runner struct {
	newHTTPClient func(config.Config) (*http.Client, error)
	loadHistory   func(string) (recordStore, error)
	saveArchive   func(string, []history.Record) error
	newSetter     func() wallpaperSetter
	statePath     string
	now           func() time.Time
}

func newRunner() runner {
	return runner{
		newHTTPClient: newHTTPClient,
		loadHistory: func(filename string) (recordStore, error) {
			return history.Load(filename)
		},
		saveArchive: archive.Save,
		newSetter: func() wallpaperSetter {
			return platform.NewWallpaperSetter()
		},
		now: time.Now,
	}
}

func (r runner) currentTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// loadAppliedState returns a zero state when the state file is unavailable so
// the caller falls back to applying the wallpaper normally.
func (r runner) loadAppliedState() appstate.State {
	if r.statePath == "" {
		return appstate.State{}
	}
	st, err := appstate.Load(r.statePath)
	if err != nil {
		slog.Warn("读取设置状态失败，将重新设置", "error", err)
	}
	return st
}

// markApplied records a successful set. A failure here only causes one extra
// idempotent set on the next check, so it must not fail the update.
func (r runner) markApplied(cfg config.Config, url, file string) {
	if cfg.Setter == "no" || r.statePath == "" {
		return
	}
	st := appstate.State{
		LastAppliedURL:  url,
		LastAppliedFile: file,
		LastAppliedAt:   r.currentTime(),
	}
	if err := appstate.Save(r.statePath, st); err != nil {
		slog.Warn("保存设置状态失败", "error", err)
	}
}

// Run parses arguments and executes either a management command or wallpaper update loop.
func Run(ctx context.Context, args []string) error {
	if fileLogging == "true" {
		configureLogging(0)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	appDir := filepath.Dir(executable)
	cfg, err := config.Load(args, appDir, os.Stdout)
	if errors.Is(err, config.ErrExit) {
		return nil
	}
	if err != nil {
		return err
	}
	configureLogging(cfg.Debug)

	if cfg.ListMarkets {
		printMarkets(os.Stdout)
		return nil
	}
	if cfg.GenerateConfig {
		if err := config.WriteFile(cfg); err != nil {
			return fmt.Errorf("生成配置: %w", err)
		}
		fmt.Println("已生成配置:", cfg.ConfigFile)
		return nil
	}
	if cfg.StartupMode != "unchanged" {
		return manageStartup(executable, cfg.StartupMode, cfg.ConfigFile)
	}
	// The windowless build registers background startup on double-click when
	// startup is not managed yet, then continues with the normal run.
	if guiBuild == "true" {
		if err := ensureStartupRegistered(platform.NewStartupManager(), executable, cfg.ConfigFile); err != nil {
			slog.Warn("注册自启动失败", "error", err)
		}
	}
	normalizeRuntimePaths(&cfg)
	if runtime.GOOS != "windows" && cfg.Setter == "win" {
		return errors.New("Go 版仅面向 Windows；当前平台可用 --setter no 测试下载")
	}
	releaseInstance, err := platform.AcquireSingleInstance(`Local\zhimoe-bingwallpaper`)
	if err != nil {
		return err
	}
	defer releaseInstance()
	if err := os.MkdirAll(cfg.OutputFolder, 0o755); err != nil {
		return fmt.Errorf("创建输出目录: %w", err)
	}
	historyPath, err := historyFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		return err
	}
	statePath, stateErr := stateFile()
	if stateErr != nil {
		slog.Warn("无法确定设置状态文件路径，将不记录今天是否已设置", "error", stateErr)
	}

	worker := newRunner()
	worker.statePath = statePath
	if cfg.Background {
		return worker.runDaemon(ctx, cfg, historyPath, args, appDir)
	}
	_, err = worker.runOnce(ctx, cfg, historyPath)
	return err
}

func (r runner) runDaemon(ctx context.Context, cfg config.Config, historyPath string, args []string, appDir string) error {
	firstRun := true
	retryDelay := minRetryDelay
	for {
		if !firstRun {
			reloaded, err := config.Load(args, appDir, io.Discard)
			if err != nil {
				slog.Warn("重新读取配置失败，继续使用上次配置", "error", err)
			} else {
				cfg = reloaded
				normalizeRuntimePaths(&cfg)
				configureLogging(cfg.Debug)
				if err := os.MkdirAll(cfg.OutputFolder, 0o755); err != nil {
					slog.Warn("创建新输出目录失败，继续重试", "error", err)
				}
			}
		}
		firstRun = false
		downloaded, err := r.runOnce(ctx, cfg, historyPath)
		wait := time.Duration(cfg.Interval) * time.Hour
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			wait = retryDelay
			slog.Error("更新失败，稍后重试", "retry_in", wait, "error", err)
			retryDelay = nextRetryDelay(retryDelay)
		} else {
			retryDelay = minRetryDelay
			if !downloaded {
				slog.Info("本轮没有新下载", "next_check", wait)
			}
		}
		if !cfg.Background {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (r runner) runOnce(ctx context.Context, cfg config.Config, historyPath string) (bool, error) {
	httpClient, err := r.newHTTPClient(cfg)
	if err != nil {
		return false, err
	}
	defer httpClient.CloseIdleConnections()
	baseURL := "https://www.bing.com"
	if cfg.Server == "china" {
		baseURL = "https://s.cn.bing.net"
	}
	if cfg.Server == "custom" {
		baseURL = cfg.CustomServer
		if !strings.Contains(baseURL, "://") {
			baseURL = "http://" + baseURL
		}
	}
	api := &bing.Client{HTTP: httpClient, BaseURL: baseURL, Country: cfg.Country, Market: cfg.Market, SizeMode: cfg.SizeMode, ImageSize: cfg.ImageSize, Collectors: cfg.Collect}
	slog.Info("查询 Bing 每日壁纸", "offset", cfg.Offset, "market", cfg.Market)
	results, err := api.Fetch(ctx, cfg.Offset)
	if err != nil {
		return false, err
	}
	store, err := r.loadHistory(historyPath)
	if err != nil {
		slog.Warn("历史文件损坏，将忽略", "error", err)
		store = history.New()
	}
	applied := appstate.State{}
	if cfg.Setter != "no" {
		applied = r.loadAppliedState()
	}
	dl := download.Downloader{HTTP: httpClient}

	for _, result := range results {
		if len(result.Assets) == 0 {
			continue
		}
		mainAsset := result.Assets[0]
		mainFile := outputName(cfg, mainAsset, 0)
		if old, ok := store.Get(mainAsset.URL); ok && old.LocalFile == mainFile && !cfg.Redownload {
			slog.Info("壁纸已经下载过", "file", mainFile)
			if cfg.Setter != "no" {
				if applied.AppliedToday(mainAsset.URL, mainFile, r.currentTime()) {
					slog.Info("今天已经设置过该壁纸，跳过设置", "file", mainFile)
					return false, nil
				}
				if err := r.newSetter().Set(mainFile); err != nil {
					return false, fmt.Errorf("设置壁纸: %w", err)
				}
				r.markApplied(cfg, mainAsset.URL, mainFile)
				slog.Info("壁纸设置完成", "file", mainFile)
			}
			return false, nil
		}

		var records []history.Record
		var wallpaperFile string
		assetIndexes := make(map[bing.AssetKind]int)
		for _, asset := range result.Assets {
			assetIndexes[asset.Kind]++
			filename := outputName(cfg, asset, assetIndexes[asset.Kind])
			slog.Info("下载资源", "url", asset.URL, "file", filename)
			keepContent := cfg.DatabaseFile != "" && !cfg.DatabaseNoImage && asset.IsRecordable()
			saved, err := dl.Save(ctx, asset.URL, filename, download.SaveOptions{
				Optional:      !asset.IsMain(),
				KeepContent:   keepContent,
				ValidateImage: true,
			})
			if err != nil {
				return false, err
			}
			if saved.Size == 0 {
				continue
			}
			if asset.IsMain() {
				wallpaperFile = filename
			}
			if asset.IsRecordable() {
				records = append(records, history.NewRecord(asset.URL, filename, result.Copyright, result.Market, result.StartTime, result.EndTime, asset.Kind == bing.AssetAccompanyImage, saved.Content))
			}
		}
		if wallpaperFile == "" {
			continue
		}
		// Commit downloads to history before setting the wallpaper so a failed
		// setter retry reuses the local file instead of re-downloading. SQLite
		// and history are optional: their failures must not prevent the actual
		// wallpaper update.
		if len(records) > 0 {
			if cfg.DatabaseFile != "" {
				if err := os.MkdirAll(filepath.Dir(cfg.DatabaseFile), 0o755); err != nil {
					slog.Warn("创建数据库目录失败，跳过归档", "error", err)
				} else if err := r.saveArchive(cfg.DatabaseFile, records); err != nil {
					slog.Warn("写入 SQLite 失败，继续设置壁纸", "error", err)
				}
			}
			store.Replace(records[0])
			if err := store.Save(historyPath); err != nil {
				slog.Warn("保存下载历史失败，继续设置壁纸", "error", err)
			}
		}
		if cfg.Setter != "no" {
			if err := r.newSetter().Set(wallpaperFile); err != nil {
				return false, fmt.Errorf("设置壁纸: %w", err)
			}
			r.markApplied(cfg, mainAsset.URL, wallpaperFile)
			slog.Info("壁纸设置完成", "file", wallpaperFile)
		}
		return true, nil
	}
	slog.Info("今天没有符合分辨率策略的壁纸")
	return false, nil
}

// ensureStartupRegistered registers background startup unless startup is
// already managed, making the GUI build self-installing on double-click.
func ensureStartupRegistered(mgr platform.StartupManager, executable, configFile string) error {
	status, err := mgr.Status()
	if err != nil {
		return err
	}
	if status == "disabled" {
		return mgr.Set(executable, "background", configFile)
	}
	return nil
}

func manageStartup(executable, mode, configFile string) error {
	mgr := platform.NewStartupManager()
	switch mode {
	case "disable":
		if err := mgr.Disable(); err != nil {
			return err
		}
		fmt.Println("已禁用当前用户的开机自启动")
	case "status":
		status, err := mgr.Status()
		if err != nil {
			return err
		}
		fmt.Println("自启动状态:", status)
	case "background", "foreground":
		if err := mgr.Set(executable, mode, configFile); err != nil {
			return err
		}
		fmt.Println("已启用当前用户自启动，模式:", mode)
	}
	return nil
}

func historyFile() (string, error) {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "Genzj", "PyBingWallpaper", "bing-wallpaper-history.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "AppData", "Roaming", "Genzj", "PyBingWallpaper", "bing-wallpaper-history.json"), nil
}

// appDataDir is the per-user directory for logs and the applied-wallpaper state.
func appDataDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "zhimoe", "BingWallpaper"), nil
}

func stateFile() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func configureLogging(debug int) {
	level := slog.LevelInfo
	if debug > 0 {
		level = slog.LevelDebug
	}
	if fileLogging == "true" {
		logWriterOnce.Do(func() {
			dir, err := appDataDir()
			if err != nil {
				return
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return
			}
			file, err := os.OpenFile(filepath.Join(dir, "bingwallpaper.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				logWriter = file
			}
		})
	}
	handler := slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

func normalizeRuntimePaths(cfg *config.Config) {
	base := filepath.Dir(cfg.ConfigFile)
	if !filepath.IsAbs(cfg.OutputFolder) {
		cfg.OutputFolder = filepath.Join(base, cfg.OutputFolder)
	}
	if cfg.DatabaseFile != "" && !filepath.IsAbs(cfg.DatabaseFile) {
		cfg.DatabaseFile = filepath.Join(base, cfg.DatabaseFile)
	}
}

func printMarkets(w io.Writer) {
	fmt.Fprintln(w, "Available markets:")
	for _, item := range markets {
		fmt.Fprintf(w, "%s    %s\n", item[0], item[1])
	}
}

var markets = [][2]string{
	{"es-AR", "Argentina"}, {"en-AU", "Australia"}, {"de-AT", "Austria"}, {"nl-BE", "Belgium - Dutch"},
	{"fr-BE", "Belgium - French"}, {"pt-BR", "Brazil"}, {"en-CA", "Canada - English"}, {"fr-CA", "Canada - French"},
	{"es-CL", "Chile"}, {"zh-CN", "China"}, {"da-DK", "Denmark"}, {"ar-EG", "Egypt"}, {"fi-FI", "Finland"},
	{"fr-FR", "France"}, {"de-DE", "Germany"}, {"zh-HK", "Hong Kong SAR"}, {"en-IN", "India"}, {"en-ID", "Indonesia"},
	{"en-IE", "Ireland"}, {"it-IT", "Italy"}, {"ja-JP", "Japan"}, {"ko-KR", "Korea"}, {"en-MY", "Malaysia"},
	{"es-MX", "Mexico"}, {"nl-NL", "Netherlands"}, {"en-NZ", "New Zealand"}, {"nb-NO", "Norway"},
	{"en-PH", "Philippines"}, {"pl-PL", "Poland"}, {"pt-PT", "Portugal"}, {"ru-RU", "Russia"},
	{"ar-SA", "Saudi Arabia"}, {"en-SG", "Singapore"}, {"en-ZA", "South Africa"}, {"es-ES", "Spain"},
	{"sv-SE", "Sweden"}, {"fr-CH", "Switzerland - French"}, {"de-CH", "Switzerland - German"},
	{"zh-TW", "Taiwan"}, {"tr-TR", "Turkey"}, {"ar-AE", "United Arab Emirates"}, {"en-GB", "United Kingdom"},
	{"en-US", "United States - English"}, {"es-US", "United States - Spanish"},
}
