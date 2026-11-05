package bing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/httpx"
)

// Client retrieves wallpaper metadata and selects assets from a Bing endpoint.
type Client struct {
	HTTP       *http.Client
	BaseURL    string
	Country    string
	Market     string
	SizeMode   string
	ImageSize  string
	Collectors []string
}

// AssetKind identifies how a downloaded asset is used and named.
type AssetKind string

const (
	// AssetMainImage is the image applied as the desktop wallpaper.
	AssetMainImage AssetKind = "main-image"
	// AssetAccompanyImage is an alternate branded version of the main image.
	AssetAccompanyImage AssetKind = "accompany-image"
)

// Asset describes one downloadable resource returned by Bing.
type Asset struct {
	URL  string
	Kind AssetKind
}

// IsMain reports whether the asset should be applied as wallpaper.
func (a Asset) IsMain() bool { return a.Kind == AssetMainImage }

// IsRecordable reports whether the asset belongs in the compatible image archive.
func (a Asset) IsRecordable() bool { return a.Kind == AssetMainImage || a.Kind == AssetAccompanyImage }

// Result groups the selected assets and metadata for one Bing image entry.
type Result struct {
	Assets        []Asset
	Copyright     string
	CopyrightLink string
	Market        string
	StartTime     time.Time
	EndTime       time.Time
}

type archive struct {
	Images []imageInfo `json:"images"`
	Market struct {
		Mkt string `json:"mkt"`
	} `json:"market"`
}

type imageInfo struct {
	StartDate     string `json:"startdate"`
	FullStartDate string `json:"fullstartdate"`
	EndDate       string `json:"enddate"`
	URL           string `json:"url"`
	URLBase       string `json:"urlbase"`
	Copyright     string `json:"copyright"`
	CopyrightLink string `json:"copyrightlink"`
	Hash          string `json:"hsh"`
	Wallpaper     bool   `json:"wp"`
}

// Fetch retrieves and selects the wallpaper at the requested day offset.
func (c *Client) Fetch(ctx context.Context, offset int) ([]Result, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Bing 根地址: %w", err)
	}
	endpoint, _ := url.Parse("/HPImageArchive.aspx")
	u := base.ResolveReference(endpoint)
	q := u.Query()
	q.Set("format", "js")
	q.Set("mbl", "1")
	q.Set("idx", fmt.Sprint(offset))
	q.Set("n", "1")
	if c.Market != "" {
		q.Set("mkt", c.Market)
	} else if c.Country != "" && c.Country != "auto" {
		q.Set("cc", c.Country)
	}
	u.RawQuery = q.Encode()

	var payload archive
	if err := c.getJSON(ctx, u.String(), &payload); err != nil {
		return nil, err
	}
	if len(payload.Images) == 0 {
		return nil, errors.New("Bing API 未返回壁纸")
	}
	market := payload.Market.Mkt
	if market == "" {
		market = c.Market
	}
	results := make([]Result, 0, len(payload.Images))
	for _, img := range payload.Images {
		mainURL, err := c.chooseImage(ctx, base, img)
		if err != nil {
			return nil, err
		}
		if mainURL == "" {
			continue
		}
		startTime, err := parseBingTime(img.FullStartDate)
		if err != nil {
			return nil, fmt.Errorf("解析 fullstartdate %q: %w", img.FullStartDate, err)
		}
		endTime, err := parseBingDate(img.EndDate)
		if err != nil {
			return nil, fmt.Errorf("解析 enddate %q: %w", img.EndDate, err)
		}
		result := Result{
			Assets: []Asset{{URL: mainURL, Kind: AssetMainImage}}, Copyright: img.Copyright,
			CopyrightLink: img.CopyrightLink, Market: market,
			StartTime: startTime, EndTime: endTime,
		}
		result.Assets = append(result.Assets, c.collect(base, img)...)
		results = append(results, result)
	}
	return results, nil
}

func (c *Client) chooseImage(ctx context.Context, base *url.URL, img imageInfo) (string, error) {
	resolve := func(raw string) (string, error) {
		ref, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("Bing 图片地址: %w", err)
		}
		u := base.ResolveReference(ref)
		if raw == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", fmt.Errorf("无效 Bing 图片地址 %q", raw)
		}
		return u.String(), nil
	}
	withSize := func(size string) (string, error) {
		if img.URLBase == "" {
			return "", errors.New("Bing 未返回 urlbase")
		}
		return resolve(img.URLBase + "_" + size + ".jpg")
	}
	switch c.SizeMode {
	case "uhd":
		return withSize("UHD")
	case "prefer", "highest":
		// prefer probes for the best available size, highest is kept as a
		// legacy alias with identical behavior.
		for _, size := range []string{"UHD", "1920x1200", "1920x1080"} {
			candidate, err := withSize(size)
			if err != nil {
				return "", err
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if c.exists(ctx, candidate) {
				return candidate, nil
			}
		}
		return resolve(img.URL)
	case "insist":
		if !img.Wallpaper {
			return "", nil
		}
		return withSize("1920x1200")
	case "manual":
		return withSize(c.ImageSize)
	case "never":
		return resolve(img.URL)
	default:
		return "", fmt.Errorf("未知分辨率策略 %q", c.SizeMode)
	}
}

func (c *Client) collect(base *url.URL, img imageInfo) []Asset {
	wanted := make(map[string]bool, len(c.Collectors))
	for _, name := range c.Collectors {
		wanted[strings.ToLower(strings.TrimSpace(name))] = true
	}
	resolve := func(raw string) string {
		ref, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return base.ResolveReference(ref).String()
	}
	var assets []Asset
	if wanted["accompany"] && img.Wallpaper && !strings.Contains(img.URLBase, "_ZH_") {
		assets = append(assets, Asset{URL: resolve(img.URLBase + "_ZH_1920x1200.jpg"), Kind: AssetAccompanyImage})
	}
	return assets
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) exists(ctx context.Context, rawURL string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	slog.Debug("检测图片", "url", rawURL, "status", resp.StatusCode)
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (c *Client) getJSON(ctx context.Context, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("请求 Bing API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bing API 返回 %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(dst); err != nil {
		return fmt.Errorf("解析 Bing API: %w", err)
	}
	return nil
}

// FileName derives a Windows-safe filename from a Bing asset URL. Bing's
// thumbnail endpoint serves real assets through the id query parameter, e.g.
// /th?id=OHR.Example_zh-CN123456_1920x1080, which carries no extension.
func FileName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "wallpaper.jpg"
	}
	name := path.Base(u.Path)
	fromID := false
	if name == "th" || name == "." || name == "/" || name == "" {
		name = u.Query().Get("id")
		fromID = true
	}
	if name == "" {
		name = "bingwallpaper_" + time.Now().Format("2006-01-02_150405") + ".jpg"
	} else if fromID && !strings.HasSuffix(strings.ToLower(name), ".jpg") {
		// The id parameter is a bare resource name; images are always JPEG.
		// HasSuffix is used because the id itself contains a dot (OHR.Name_...).
		name += ".jpg"
	}
	return filepathSafe(name)
}

func filepathSafe(name string) string {
	replacer := strings.NewReplacer("<", "_", ">", "_", ":", "_", `"`, "_", "/", "_", `\`, "_", "|", "_", "?", "_", "*", "_")
	return replacer.Replace(name)
}

func parseBingTime(v string) (time.Time, error) { return time.Parse("200601021504", v) }
func parseBingDate(v string) (time.Time, error) { return time.Parse("20060102", v) }
