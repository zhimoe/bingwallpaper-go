package bing

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestFetchPreferAndCollect(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusNotFound
		body := ""
		switch {
		case r.URL.Path == "/HPImageArchive.aspx":
			if r.URL.Query().Get("mkt") != "zh-CN" || r.URL.Query().Get("idx") != "2" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			status = http.StatusOK
			body = `{"market":{"mkt":"zh-CN"},"images":[{"startdate":"20260810","fullstartdate":"202608100800","enddate":"20260811","url":"/fallback.jpg","urlbase":"/image","copyright":"测试图片","copyrightlink":"https://example.test","hsh":"abc","wp":true,"vid":{"sources":[["mp4","x","/video.mp4"],["mp4hd","x","/video-hd.mp4"]]}}]}`
		case r.Method == http.MethodHead && strings.Contains(r.URL.Path, "UHD"):
			status = http.StatusNotFound
		case r.Method == http.MethodHead && strings.Contains(r.URL.Path, "1920x1200"):
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}

	c := Client{HTTP: httpClient, BaseURL: "https://bing.test", Market: "zh-CN", SizeMode: "prefer", Collectors: []string{"accompany", "video", "hdvideo"}}
	results, err := c.Fetch(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	got := results[0]
	if !strings.Contains(got.Assets[0].URL, "1920x1200") {
		t.Fatalf("main = %s", got.Assets[0].URL)
	}
	if len(got.Assets) != 2 || got.Assets[1].Kind != AssetAccompanyImage || strings.Contains(got.Assets[1].URL, "video") {
		t.Fatalf("assets = %#v", got.Assets)
	}
	if got.Market != "zh-CN" || got.Copyright != "测试图片" || got.StartTime.IsZero() {
		t.Fatalf("metadata = %#v", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFileNameNewBingURL(t *testing.T) {
	got := FileName("https://www.bing.com/th?id=OHR.Example_UHD.jpg&rf=LaDigue_UHD.jpg")
	if got != "OHR.Example_UHD.jpg" {
		t.Fatalf("filename = %q", got)
	}
}

func TestFileNameIDWithoutExtensionGetsJPGExtension(t *testing.T) {
	got := FileName("https://www.bing.com/th?id=OHR.Example_zh-CN123456_1920x1080")
	if got != "OHR.Example_zh-CN123456_1920x1080.jpg" {
		t.Fatalf("filename = %q", got)
	}
}

func TestChooseImageRejectsInvalidURLs(t *testing.T) {
	base, err := url.Parse("https://bing.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		mode string
		img  imageInfo
	}{
		{"never", imageInfo{URL: "%invalid"}},
		{"never", imageInfo{URL: ""}},
		{"never", imageInfo{URL: "file:///tmp/image.jpg"}},
		{"uhd", imageInfo{URLBase: "%invalid"}},
		{"uhd", imageInfo{}},
		{"prefer", imageInfo{URLBase: "%invalid"}},
	} {
		t.Run(tt.mode+tt.img.URL+tt.img.URLBase, func(t *testing.T) {
			c := Client{SizeMode: tt.mode}
			if _, err := c.chooseImage(t.Context(), base, tt.img); err == nil {
				t.Fatal("expected invalid URL error")
			}
		})
	}
}
