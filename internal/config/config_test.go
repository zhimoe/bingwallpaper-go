package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadPrecedenceAndCompatibility(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "settings.conf")
	data := `[Daemon]
background = true
interval = 4
[Download]
country = cn
market = zh-CN
size_mode = collect
collect = video
output_folder = C:\Wallpapers
[Setter]
setter = no
[Database]
database_no_image = false
`
	if err := os.WriteFile(conf, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cfg, err := Load([]string{"--config-file", conf, "--foreground", "--interval", "7", "--collect", "hdvideo", "--database-no-image"}, dir, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "视频") {
		t.Fatalf("missing video collector warning: %q", out.String())
	}
	if cfg.Background {
		t.Fatal("foreground must override background config")
	}
	if cfg.Interval != 7 {
		t.Fatalf("interval = %d", cfg.Interval)
	}
	if cfg.SizeMode != "prefer" {
		t.Fatalf("legacy collect mode = %q", cfg.SizeMode)
	}
	if !reflect.DeepEqual(cfg.Collect, []string{"accompany"}) {
		t.Fatalf("collect = %#v", cfg.Collect)
	}
	if !cfg.DatabaseNoImage {
		t.Fatal("CLI boolean did not override config")
	}
	if cfg.Market != "zh-CN" || cfg.Setter != "no" {
		t.Fatalf("file values not loaded: %#v", cfg)
	}
}

func TestEncodeDecodeINI(t *testing.T) {
	want := Defaults(t.TempDir())
	want.Background = true
	want.Collect = []string{"accompany"}
	want.DatabaseNoImage = true
	var buf bytes.Buffer
	if err := EncodeINI(&buf, want); err != nil {
		t.Fatal(err)
	}
	got := Defaults(t.TempDir())
	if err := DecodeINI(strings.NewReader(buf.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Background != want.Background || !reflect.DeepEqual(got.Collect, want.Collect) || got.DatabaseNoImage != want.DatabaseNoImage {
		t.Fatalf("round trip mismatch\nwant=%#v\ngot=%#v", want, got)
	}
}

func TestHelpAndVersionExitSuccessfully(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"-v"}} {
		var out bytes.Buffer
		_, err := Load(args, t.TempDir(), &out)
		if err != ErrExit {
			t.Fatalf("args=%v err=%v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("args=%v produced no output", args)
		}
	}
}

func TestLoadIgnoresVideoCollectors(t *testing.T) {
	var out bytes.Buffer
	cfg, err := Load([]string{"--collect", "video", "--collect", "hdvideo", "--collect", "accompany"}, t.TempDir(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Collect, []string{"accompany"}) {
		t.Fatalf("collect = %#v", cfg.Collect)
	}
	if !strings.Contains(out.String(), "已忽略 collect=video,hdvideo") {
		t.Fatalf("warning = %q", out.String())
	}
}

func TestValidateManualSize(t *testing.T) {
	cfg := Defaults(t.TempDir())
	cfg.SizeMode = "manual"
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing image size should fail")
	}
	cfg.ImageSize = "2560x1440"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateConfigIgnoresExistingValues(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "settings.conf")
	if err := os.WriteFile(conf, []byte("[Daemon]\ninterval = 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{"--config-file", conf, "--generate-config"}, dir, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 2 {
		t.Fatalf("generate should use defaults, interval=%d", cfg.Interval)
	}
}

func TestWriteFileAtomicallyReplacesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "settings.conf")
	if err := os.WriteFile(filename, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Defaults(dir)
	cfg.ConfigFile = filename
	cfg.Market = "zh-CN"
	if err := WriteFile(cfg); err != nil {
		t.Fatal(err)
	}
	loaded := Defaults(dir)
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := DecodeINI(file, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Market != "zh-CN" {
		t.Fatalf("market = %q", loaded.Market)
	}
}

func TestValidateRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"market", func(c *Config) { c.Market = "z_-??" }},
		{"manual size", func(c *Config) { c.SizeMode, c.ImageSize = "manual", "0x-1" }},
		{"collector", func(c *Config) { c.Collect = []string{"unknown"} }},
		{"proxy port", func(c *Config) { c.ProxyServer, c.ProxyPort = "http://proxy.test", "70000" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults(t.TempDir())
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
