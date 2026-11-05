package app

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/config"
)

// newHTTPClient builds the shared HTTP client: a cloned default transport with
// an explicit dial timeout and optional proxy configuration.
func newHTTPClient(cfg config.Config) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	if cfg.ProxyServer != "" {
		raw := cfg.ProxyServer
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("代理地址: %w", err)
		}
		if u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("无效代理地址: %s", cfg.ProxyServer)
		}
		if u.Port() == "" && cfg.ProxyPort != "" {
			u.Host = net.JoinHostPort(u.Hostname(), cfg.ProxyPort)
		}
		if cfg.ProxyUsername != "" {
			u.User = url.UserPassword(cfg.ProxyUsername, cfg.ProxyPassword)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Minute}, nil
}
