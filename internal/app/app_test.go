package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/bing"
	"github.com/zhimoe/bingwallpaper/internal/config"
	"github.com/zhimoe/bingwallpaper/internal/history"
	appstate "github.com/zhimoe/bingwallpaper/internal/state"
)

const (
	mainImageBody      = "\xff\xd8\xff\xe0main image"
	accompanyImageBody = "\xff\xd8\xff\xe0accompany image"
)

var testNow = time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)

type appRoundTripFunc func(*http.Request) (*http.Response, error)

func (f appRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeStore struct {
	records map[string]history.Record
	events  *[]string
	saveErr error
}

func (s *fakeStore) Get(rawURL string) (history.Record, bool) {
	r, ok := s.records[rawURL]
	return r, ok
}
func (s *fakeStore) Replace(record history.Record) {
	s.records = map[string]history.Record{record.URL: record}
}
func (s *fakeStore) Save(string) error {
	*s.events = append(*s.events, "history")
	return s.saveErr
}

type fakeSetter struct {
	events *[]string
	err    error
}

func (s fakeSetter) Set(filename string) error {
	data, readErr := os.ReadFile(filename)
	if readErr != nil {
		return readErr
	}
	if !bytes.Contains(data, []byte("main image")) {
		return errors.New("the main wallpaper was overwritten")
	}
	*s.events = append(*s.events, "setter")
	return s.err
}

func TestRunOnceKeepsAssetsSeparateAndCommitsInOrder(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})
	worker.saveArchive = func(_ string, records []history.Record) error {
		events = append(events, "archive")
		if len(records) != 2 || string(records[0].Raw) != mainImageBody || string(records[1].Raw) != accompanyImageBody {
			t.Fatalf("records = %#v", records)
		}
		return nil
	}

	downloaded, err := worker.runOnce(context.Background(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !downloaded {
		t.Fatal("expected a download")
	}
	if !reflect.DeepEqual(events, []string{"archive", "history", "setter"}) {
		t.Fatalf("events = %#v", events)
	}
	assertFileContent(t, filepath.Join(cfg.OutputFolder, "wallpaper.jpg"), mainImageBody)
	assertFileContent(t, filepath.Join(cfg.OutputFolder, "wallpaper-accompany.jpg"), accompanyImageBody)
}

func TestRunOnceCommitsHistoryBeforeSetterFails(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events, err: errors.New("setter failed")})
	_, err := worker.runOnce(context.Background(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err == nil || !strings.Contains(err.Error(), "setter failed") {
		t.Fatalf("error = %v", err)
	}
	// The download is committed before the setter runs so a retry reuses the
	// local file instead of downloading again.
	if !reflect.DeepEqual(events, []string{"history", "setter"}) {
		t.Fatalf("events = %#v", events)
	}
	if _, ok := store.Get("https://bing.test/image_UHD.jpg"); !ok {
		t.Fatal("history should record the download even when the setter fails")
	}
}

func TestOutputNameUsesAssetKind(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	assets := []struct {
		asset bing.Asset
		index int
		want  string
	}{
		{bing.Asset{URL: "https://bing.test/main.jpg", Kind: bing.AssetMainImage}, 1, "wallpaper.jpg"},
		{bing.Asset{URL: "https://bing.test/accompany.jpg", Kind: bing.AssetAccompanyImage}, 1, "wallpaper-accompany.jpg"},
	}
	for _, tt := range assets {
		if got := filepath.Base(outputName(cfg, tt.asset, tt.index)); got != tt.want {
			t.Errorf("outputName(%s) = %s, want %s", tt.asset.Kind, got, tt.want)
		}
	}
}

func TestNormalizeRuntimePathsUsesConfigDirectory(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.ConfigFile = filepath.Join(t.TempDir(), "settings.conf")
	cfg.OutputFolder = "images"
	cfg.DatabaseFile = filepath.Join("data", "archive.db")
	normalizeRuntimePaths(&cfg)
	base := filepath.Dir(cfg.ConfigFile)
	if cfg.OutputFolder != filepath.Join(base, "images") || cfg.DatabaseFile != filepath.Join(base, "data", "archive.db") {
		t.Fatalf("paths not normalized: %#v", cfg)
	}
}

func TestHTTPClientBuildsIPv6ProxyAddress(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.ProxyServer = "http://[::1]"
	cfg.ProxyPort = "8080"
	client, err := newHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	request, _ := http.NewRequest(http.MethodGet, "https://bing.test", nil)
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.Host != "[::1]:8080" {
		t.Fatalf("proxy host = %q", proxyURL.Host)
	}
}

func TestHTTPClientRejectsInvalidProxy(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.ProxyServer = "ftp://proxy.test"
	if _, err := newHTTPClient(cfg); err == nil {
		t.Fatal("expected invalid proxy error")
	}
}

type fakeStartupManager struct {
	status string
	setErr error
	calls  []string
}

func (m *fakeStartupManager) Set(executable, mode, configFile string) error {
	m.calls = append(m.calls, "set:"+mode)
	return m.setErr
}
func (m *fakeStartupManager) Disable() error { m.calls = append(m.calls, "disable"); return nil }
func (m *fakeStartupManager) Status() (string, error) {
	m.calls = append(m.calls, "status")
	return m.status, nil
}

func TestEnsureStartupRegisteredRegistersWhenDisabled(t *testing.T) {
	mgr := &fakeStartupManager{status: "disabled"}
	if err := ensureStartupRegistered(mgr, "C:\\app\\BingWallpaper.exe", "C:\\app\\settings.conf"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mgr.calls, []string{"status", "set:background"}) {
		t.Fatalf("calls = %#v", mgr.calls)
	}
}

func TestEnsureStartupRegisteredSkipsWhenAlreadyManaged(t *testing.T) {
	mgr := &fakeStartupManager{status: "background: C:\\app\\BingWallpaper.exe --background"}
	if err := ensureStartupRegistered(mgr, "C:\\app\\BingWallpaper.exe", "C:\\app\\settings.conf"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mgr.calls, []string{"status"}) {
		t.Fatalf("calls = %#v", mgr.calls)
	}
}

func testRunner(t *testing.T, store recordStore, setter wallpaperSetter) (runner, config.Config) {
	t.Helper()
	client := &http.Client{Transport: appRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, status := "", http.StatusNotFound
		switch r.URL.Path {
		case "/HPImageArchive.aspx":
			status = http.StatusOK
			body = `{"market":{"mkt":"zh-CN"},"images":[{"fullstartdate":"202608100800","enddate":"20260811","url":"/fallback.jpg","urlbase":"/image","copyright":"test","wp":true}]}`
		case "/image_UHD.jpg":
			status, body = http.StatusOK, mainImageBody
		case "/image_ZH_1920x1200.jpg":
			status, body = http.StatusOK, accompanyImageBody
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	worker := runner{
		newHTTPClient: func(config.Config) (*http.Client, error) { return client, nil },
		loadHistory:   func(string) (recordStore, error) { return store, nil },
		saveArchive:   func(string, []history.Record) error { return nil },
		newSetter:     func() wallpaperSetter { return setter },
		statePath:     filepath.Join(t.TempDir(), "state.json"),
		now:           func() time.Time { return testNow },
	}
	cfg := config.Defaults(t.TempDir())
	cfg.OutputFolder = t.TempDir()
	cfg.Server = "custom"
	cfg.CustomServer = "https://bing.test"
	cfg.Market = "zh-CN"
	cfg.SizeMode = "uhd"
	cfg.Collect = []string{"accompany"}
	cfg.Setter = "win"
	cfg.DatabaseFile = filepath.Join(t.TempDir(), "archive.db")
	return worker, cfg
}

func assertFileContent(t *testing.T, filename, want string) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", filename, data, want)
	}
}

func TestRunOnceRetriesSetterWithoutDownloading(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events, err: errors.New("setter failed")})
	filename := filepath.Join(t.TempDir(), "history.json")
	if _, err := worker.runOnce(t.Context(), cfg, filename); err == nil {
		t.Fatal("expected setter failure")
	}
	client, err := worker.newHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	original := client.Transport
	client.Transport = appRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/HPImageArchive.aspx" {
			t.Fatalf("retry downloaded asset: %s", req.URL)
		}
		return original.RoundTrip(req)
	})
	worker.newSetter = func() wallpaperSetter { return fakeSetter{events: &events} }
	events = nil
	downloaded, err := worker.runOnce(t.Context(), cfg, filename)
	if err != nil || downloaded {
		t.Fatalf("downloaded = %t, err = %v", downloaded, err)
	}
	if !reflect.DeepEqual(events, []string{"setter"}) {
		t.Fatalf("retry events = %v", events)
	}
	cfg.Setter = "no"
	events = nil
	if _, err := worker.runOnce(t.Context(), cfg, filename); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("download-only cache hit events = %v", events)
	}
}

func TestRunOnceSkipsSetterWhenAppliedToday(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})
	mainURL := "https://bing.test/image_UHD.jpg"
	mainFile := filepath.Join(cfg.OutputFolder, "wallpaper.jpg")
	store.records[mainURL] = history.Record{URL: mainURL, LocalFile: mainFile}
	if err := appstate.Save(worker.statePath, appstate.State{
		LastAppliedURL:  mainURL,
		LastAppliedFile: mainFile,
		LastAppliedAt:   testNow.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	downloaded, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if downloaded {
		t.Fatal("cached wallpaper should not be reported as downloaded")
	}
	if len(events) != 0 {
		t.Fatalf("setter must not run again today: %v", events)
	}
}

func TestRunOnceAppliesWhenAppliedYesterday(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})
	mainURL := "https://bing.test/image_UHD.jpg"
	mainFile := filepath.Join(cfg.OutputFolder, "wallpaper.jpg")
	if err := os.WriteFile(mainFile, []byte(mainImageBody), 0o600); err != nil {
		t.Fatal(err)
	}
	store.records[mainURL] = history.Record{URL: mainURL, LocalFile: mainFile}
	if err := appstate.Save(worker.statePath, appstate.State{
		LastAppliedURL:  mainURL,
		LastAppliedFile: mainFile,
		LastAppliedAt:   testNow.AddDate(0, 0, -1),
	}); err != nil {
		t.Fatal(err)
	}

	downloaded, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if downloaded {
		t.Fatal("history hit should not report a download")
	}
	if !reflect.DeepEqual(events, []string{"setter"}) {
		t.Fatalf("events = %v", events)
	}
	state, err := appstate.Load(worker.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.AppliedToday(mainURL, mainFile, testNow) {
		t.Fatalf("state was not refreshed: %#v", state)
	}
}

func TestRunOnceWritesAppliedStateAfterDownload(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})

	downloaded, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !downloaded {
		t.Fatal("expected a download")
	}
	state, err := appstate.Load(worker.statePath)
	if err != nil {
		t.Fatal(err)
	}
	mainFile := filepath.Join(cfg.OutputFolder, "wallpaper.jpg")
	if !state.AppliedToday("https://bing.test/image_UHD.jpg", mainFile, testNow) {
		t.Fatalf("state = %#v", state)
	}
}

func TestRunOnceSetterFailureDoesNotMarkApplied(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events, err: errors.New("setter failed")})

	if _, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json")); err == nil {
		t.Fatal("expected setter failure")
	}
	state, err := appstate.Load(worker.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAppliedURL != "" || state.AppliedToday("https://bing.test/image_UHD.jpg", filepath.Join(cfg.OutputFolder, "wallpaper.jpg"), testNow) {
		t.Fatalf("failed set must not be recorded: %#v", state)
	}
}

func TestRunOnceHistorySaveFailureStillSetsWallpaper(t *testing.T) {
	var events []string
	store := &fakeStore{
		records: make(map[string]history.Record),
		events:  &events,
		saveErr: errors.New("history failed"),
	}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})

	downloaded, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatalf("history failure must not fail the update: %v", err)
	}
	if !downloaded {
		t.Fatal("expected a download")
	}
	if !reflect.DeepEqual(events, []string{"history", "setter"}) {
		t.Fatalf("events = %v", events)
	}
}

func TestRunOnceArchiveFailureStillSetsWallpaper(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})
	worker.saveArchive = func(string, []history.Record) error {
		events = append(events, "archive")
		return errors.New("archive failed")
	}

	downloaded, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatalf("archive failure must not fail the update: %v", err)
	}
	if !downloaded {
		t.Fatal("expected a download")
	}
	if !reflect.DeepEqual(events, []string{"archive", "history", "setter"}) {
		t.Fatalf("events = %v", events)
	}
}

func TestRunOnceCorruptStateStillSetsWallpaper(t *testing.T) {
	var events []string
	store := &fakeStore{records: make(map[string]history.Record), events: &events}
	worker, cfg := testRunner(t, store, fakeSetter{events: &events})
	if err := os.WriteFile(worker.statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	downloaded, err := worker.runOnce(t.Context(), cfg, filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !downloaded {
		t.Fatal("expected a download")
	}
	if !reflect.DeepEqual(events, []string{"history", "setter"}) {
		t.Fatalf("events = %v", events)
	}
	state, err := appstate.Load(worker.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.AppliedToday("https://bing.test/image_UHD.jpg", filepath.Join(cfg.OutputFolder, "wallpaper.jpg"), testNow) {
		t.Fatalf("corrupt state was not replaced: %#v", state)
	}
}
