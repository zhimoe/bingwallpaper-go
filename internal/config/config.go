package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"github.com/zhimoe/bingwallpaper/internal/fsutil"
)

// Version is the user-visible application version.
const Version = "2.0.0-go"

var marketPattern = regexp.MustCompile(`^[A-Za-z]{2}-[A-Za-z]{2}$`)

// Config contains effective runtime settings after file and CLI merging.
type Config struct {
	ConfigFile      string
	GenerateConfig  bool
	Background      bool
	Foreground      bool
	Country         string
	Market          string
	ListMarkets     bool
	Debug           int
	Interval        int
	KeepFileName    bool
	SizeMode        string
	Collect         []string
	ImageSize       string
	Offset          int
	ProxyServer     string
	ProxyPort       string
	ProxyUsername   string
	ProxyPassword   string
	Redownload      bool
	Setter          string
	SetterArgs      []string
	OutputFolder    string
	DatabaseFile    string
	DatabaseNoImage bool
	Server          string
	CustomServer    string
	StartupMode     string
}

// Defaults returns the Windows-oriented default configuration.
func Defaults(appDir string) Config {
	home, _ := os.UserHomeDir()
	return Config{
		ConfigFile: filepath.Join(appDir, "settings.conf"), Country: "auto",
		Interval: 2, SizeMode: "prefer", ProxyPort: "80", Setter: "win",
		OutputFolder: filepath.Join(home, "MyBingWallpapers"), Server: "global",
		StartupMode: "unchanged",
	}
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
func (s *stringSlice) Type() string { return "stringArray" }

// Load merges defaults, an optional INI file, and explicitly supplied CLI flags.
func Load(args []string, appDir string, out io.Writer) (Config, error) {
	defaults := Defaults(appDir)
	cli := defaults
	fs := newFlagSet(&cli, out)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.Changed("help") {
		fs.Usage()
		return Config{}, ErrExit
	}

	if flag := fs.Lookup("version"); flag != nil && flag.Changed {
		fmt.Fprintf(out, "bingwallpaper %s (Go rewrite of PyBingWallpaper)\n", Version)
		return Config{}, ErrExit
	}

	// The config path must be known before loading the rest of the file.
	configPath := defaults.ConfigFile
	if fs.Changed("config-file") {
		configPath = cli.ConfigFile
	}
	run := defaults
	run.ConfigFile = configPath
	// Match the Python version: --generate-config starts from defaults and CLI
	// values instead of silently copying an existing file back onto itself.
	if !cli.GenerateConfig {
		if file, err := os.Open(configPath); err == nil {
			defer file.Close()
			if err := DecodeINI(file, &run); err != nil {
				return Config{}, fmt.Errorf("读取配置 %s: %w", configPath, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("打开配置 %s: %w", configPath, err)
		}
	}

	applyChanged(fs, &run, cli)
	if run.Foreground {
		run.Background = false
	}
	if run.Interval < 1 {
		run.Interval = 1
	}
	if run.SizeMode == "collect" {
		run.SizeMode = "prefer"
		run.Collect = append(run.Collect, "accompany")
	}
	var dropped []string
	run.Collect, dropped = filterCollectors(run.Collect)
	if len(dropped) > 0 && out != nil {
		fmt.Fprintf(out, "提示：Go 版不再下载 MP4 视频，已忽略 collect=%s\n", strings.Join(dropped, ","))
	}
	if err := run.Validate(); err != nil {
		return Config{}, err
	}
	return run, nil
}

// ErrExit indicates that help or version output completed successfully.
var ErrExit = errors.New("requested successful exit")

func newFlagSet(c *Config, out io.Writer) *pflag.FlagSet {
	fs := pflag.NewFlagSet("bingwallpaper", pflag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		fmt.Fprintln(out, "用法: bingwallpaper [选项]\n下载 Bing 每日壁纸，并在 Windows 上设为桌面背景。")
		fs.PrintDefaults()
	}
	fs.BoolP("help", "h", false, "显示帮助")
	fs.BoolP("version", "v", false, "显示版本")
	fs.StringVar(&c.ConfigFile, "config-file", c.ConfigFile, "配置文件路径（默认与程序同目录 settings.conf）")
	fs.BoolVar(&c.GenerateConfig, "generate-config", c.GenerateConfig, "生成配置文件后退出")
	fs.BoolVarP(&c.Background, "background", "b", c.Background, "后台定时检查")
	fs.BoolVar(&c.Foreground, "foreground", c.Foreground, "强制单次前台运行")
	fs.StringVarP(&c.Country, "country", "c", c.Country, "旧式国家代码: au/br/ca/cn/de/fr/jp/nz/us/uk/auto")
	fs.StringVar(&c.Market, "market", c.Market, "市场代码，例如 zh-CN 或 en-US")
	fs.BoolVar(&c.ListMarkets, "list-markets", c.ListMarkets, "列出可用市场后退出")
	fs.CountVarP(&c.Debug, "debug", "d", "增加日志详细度（可重复）")
	fs.IntVarP(&c.Interval, "interval", "i", c.Interval, "后台检查间隔（小时，最小 1）")
	fs.BoolVarP(&c.KeepFileName, "keep-file-name", "k", c.KeepFileName, "保留 Bing 原始文件名")
	fs.StringVarP(&c.SizeMode, "size-mode", "m", c.SizeMode, "分辨率策略: prefer/highest/insist/manual/never/uhd/collect")
	fs.Var((*stringSlice)(&c.Collect), "collect", "额外收集 accompany（视频资源已不再下载，可重复）")
	fs.StringVar(&c.ImageSize, "image-size", c.ImageSize, "manual 模式分辨率，例如 1920x1080")
	fs.IntVarP(&c.Offset, "offset", "o", c.Offset, "下载 N 天前的壁纸")
	fs.StringVar(&c.ProxyServer, "proxy-server", c.ProxyServer, "代理地址，例如 http://10.1.1.1")
	fs.StringVar(&c.ProxyPort, "proxy-port", c.ProxyPort, "代理端口")
	fs.StringVar(&c.ProxyUsername, "proxy-username", c.ProxyUsername, "代理用户名")
	fs.StringVar(&c.ProxyPassword, "proxy-password", c.ProxyPassword, "代理密码")
	fs.BoolVar(&c.Redownload, "redownload", c.Redownload, "忽略历史并重新下载")
	fs.StringVarP(&c.Setter, "setter", "s", c.Setter, "壁纸设置器: win/no")
	fs.Var((*stringSlice)(&c.SetterArgs), "setter-args", "保留用于兼容旧配置（可重复）")
	fs.StringVarP(&c.OutputFolder, "output-folder", "t", c.OutputFolder, "图片保存目录")
	fs.StringVar(&c.DatabaseFile, "database-file", c.DatabaseFile, "可选 SQLite 归档文件")
	fs.BoolVar(&c.DatabaseNoImage, "database-no-image", c.DatabaseNoImage, "SQLite 中不保存图片二进制")
	fs.StringVar(&c.Server, "server", c.Server, "Bing 服务器: global/china/custom")
	fs.StringVar(&c.CustomServer, "custom-server", c.CustomServer, "自定义 Bing 服务根地址")
	fs.StringVar(&c.StartupMode, "startup-mode", c.StartupMode, "管理当前用户自启动后退出: background/foreground/disable/status")
	return fs
}

// applyChanged copies every CLI flag that was explicitly set from src into
// dst. Flag names are the kebab-case form of the Config field names, so adding
// a new flag and field requires no extra bookkeeping here.
func applyChanged(fs *pflag.FlagSet, dst *Config, src Config) {
	ddst := reflect.ValueOf(dst).Elem()
	ssrc := reflect.ValueOf(src)
	for i := 0; i < ddst.NumField(); i++ {
		field := ddst.Type().Field(i)
		if !fs.Changed(kebabCase(field.Name)) {
			continue
		}
		value := ssrc.Field(i)
		if value.Kind() == reflect.Slice {
			// Copy the slice so later appends (e.g. legacy collect migration)
			// never alias the CLI parse result.
			copied := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			reflect.Copy(copied, value)
			ddst.Field(i).Set(copied)
			continue
		}
		ddst.Field(i).Set(value)
	}
}

// kebabCase converts a CamelCase identifier to its kebab-case flag name,
// e.g. KeepFileName -> keep-file-name.
func kebabCase(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			if b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Validate checks cross-field constraints after configuration merging.
func (c Config) Validate() error {
	if !oneOf(c.Country, "au", "br", "ca", "cn", "de", "fr", "jp", "nz", "us", "uk", "auto") {
		return fmt.Errorf("无效 country: %s", c.Country)
	}
	if c.Market != "" && !marketPattern.MatchString(c.Market) {
		return fmt.Errorf("无效 market %q，应类似 zh-CN", c.Market)
	}
	if !oneOf(c.SizeMode, "prefer", "highest", "insist", "manual", "never", "uhd") {
		return fmt.Errorf("无效 size-mode: %s", c.SizeMode)
	}
	if c.SizeMode == "manual" {
		parts := strings.Split(c.ImageSize, "x")
		if len(parts) != 2 {
			return fmt.Errorf("manual 模式需要 --image-size 宽x高")
		}
		width, err := strconv.Atoi(parts[0])
		if err != nil || width <= 0 {
			return fmt.Errorf("无效 image-size: %s", c.ImageSize)
		}
		height, err := strconv.Atoi(parts[1])
		if err != nil || height <= 0 {
			return fmt.Errorf("无效 image-size: %s", c.ImageSize)
		}
	}
	for _, collector := range c.Collect {
		if !oneOf(strings.ToLower(collector), "accompany") {
			return fmt.Errorf("无效 collect: %s（Go 版仅支持 accompany）", collector)
		}
	}
	if c.ProxyServer != "" {
		port, err := strconv.Atoi(c.ProxyPort)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("无效 proxy-port: %s", c.ProxyPort)
		}
	}
	if !oneOf(c.Setter, "win", "no") {
		return fmt.Errorf("无效 setter: %s", c.Setter)
	}
	if !oneOf(c.Server, "global", "china", "custom") {
		return fmt.Errorf("无效 server: %s", c.Server)
	}
	if c.Server == "custom" && strings.TrimSpace(c.CustomServer) == "" {
		return errors.New("server=custom 时必须设置 custom-server")
	}
	if !oneOf(c.StartupMode, "unchanged", "background", "foreground", "disable", "status") {
		return fmt.Errorf("无效 startup-mode: %s", c.StartupMode)
	}
	return nil
}

// DecodeINI overlays supported legacy INI values onto c.
func DecodeINI(r io.Reader, c *Config) error {
	section := "DEFAULT"
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("第 %d 行不是 key=value", lineNo)
		}
		if err := setINI(c, section, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])); err != nil {
			return fmt.Errorf("第 %d 行: %w", lineNo, err)
		}
	}
	return scanner.Err()
}

func setINI(c *Config, section, key, value string) error {
	section, key = strings.ToLower(section), strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	b := func() (bool, error) { return strconv.ParseBool(value) }
	i := func() (int, error) { return strconv.Atoi(value) }
	switch section + "/" + key {
	case "daemon/background":
		v, e := b()
		c.Background = v
		return e
	case "daemon/interval":
		v, e := i()
		c.Interval = v
		return e
	case "download/country":
		c.Country = value
	case "download/market":
		c.Market = value
	case "download/keep_file_name":
		v, e := b()
		c.KeepFileName = v
		return e
	case "download/size_mode":
		c.SizeMode = value
	case "download/collect":
		c.Collect = splitCSV(value)
	case "download/image_size":
		c.ImageSize = value
	case "download/offset":
		v, e := i()
		c.Offset = v
		return e
	case "download/redownload":
		v, e := b()
		c.Redownload = v
		return e
	case "download/output_folder":
		c.OutputFolder = value
	case "download/server":
		c.Server = value
	case "download/customserver", "download/custom_server":
		c.CustomServer = value
	case "proxy/proxy_server":
		c.ProxyServer = value
	case "proxy/proxy_port":
		c.ProxyPort = value
	case "proxy/proxy_username":
		c.ProxyUsername = value
	case "proxy/proxy_password":
		c.ProxyPassword = value
	case "setter/setter":
		c.Setter = value
	case "setter/setter_args":
		c.SetterArgs = splitCSV(value)
	case "database/database_file":
		c.DatabaseFile = value
	case "database/database_no_image":
		v, e := b()
		c.DatabaseNoImage = v
		return e
	case "debug/debug":
		v, e := i()
		c.Debug = v
		return e
	}
	return nil
}

// EncodeINI writes the legacy-compatible settings format.
func EncodeINI(w io.Writer, c Config) error {
	_, err := fmt.Fprintf(w, `[Daemon]
background = %t
interval = %d

[Download]
country = %s
market = %s
keep_file_name = %t
size_mode = %s
collect = %s
image_size = %s
offset = %d
redownload = %t
output_folder = %s
server = %s
customserver = %s

[Proxy]
proxy_server = %s
proxy_port = %s
proxy_username = %s
proxy_password = %s

[Setter]
setter = %s
setter_args = %s

[Database]
database_file = %s
database_no_image = %t

[Debug]
debug = %d
`, c.Background, c.Interval, c.Country, c.Market, c.KeepFileName, c.SizeMode,
		strings.Join(c.Collect, ","), c.ImageSize, c.Offset, c.Redownload, c.OutputFolder,
		c.Server, c.CustomServer, c.ProxyServer, c.ProxyPort, c.ProxyUsername,
		c.ProxyPassword, c.Setter, strings.Join(c.SetterArgs, ","), c.DatabaseFile,
		c.DatabaseNoImage, c.Debug)
	return err
}

// WriteFile atomically writes c to its configured file with private permissions.
func WriteFile(c Config) error {
	dir := filepath.Dir(c.ConfigFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*.conf")
	if err != nil {
		return err
	}
	tempName := f.Name()
	defer os.Remove(tempName)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := EncodeINI(f, c); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return fsutil.ReplaceFile(tempName, c.ConfigFile)
}

func splitCSV(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// filterCollectors keeps the image collectors supported by this build. Legacy
// video collectors are dropped so old settings.conf files still load; unknown
// values are preserved so Validate can report them.
func filterCollectors(values []string) (kept, dropped []string) {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		name := strings.ToLower(strings.TrimSpace(value))
		switch name {
		case "accompany":
			if !seen[name] {
				kept = append(kept, name)
				seen[name] = true
			}
		case "video", "hdvideo":
			if !seen[name] {
				dropped = append(dropped, name)
				seen[name] = true
			}
		default:
			kept = append(kept, value)
		}
	}
	return kept, dropped
}

func oneOf(s string, values ...string) bool { return contains(values, s) }
func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
