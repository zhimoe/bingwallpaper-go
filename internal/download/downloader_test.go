package download

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSaveStreamsAndReplacesDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "wallpaper.jpg")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusOK, "new wallpaper"), nil
	})}

	result, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/image.jpg", destination, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != int64(len("new wallpaper")) || result.Content != nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new wallpaper" {
		t.Fatalf("destination = %q", data)
	}
}

func TestSaveKeepsContentOnlyWhenRequested(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusOK, "database image"), nil
	})}
	result, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/image.jpg", filepath.Join(t.TempDir(), "image.jpg"), SaveOptions{KeepContent: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Content) != "database image" {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestSaveOptionalMissingAsset(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusNotFound, "missing"), nil
	})}
	result, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/missing.jpg", filepath.Join(t.TempDir(), "missing.jpg"), SaveOptions{Optional: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSaveDoesNotReplaceWithEmptyResponse(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "wallpaper.jpg")
	if err := os.WriteFile(destination, []byte("valid old wallpaper"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusOK, ""), nil
	})}
	_, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/empty.jpg", destination, SaveOptions{})
	if err == nil {
		t.Fatal("expected empty response error")
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "valid old wallpaper" {
		t.Fatalf("destination was replaced: %q", data)
	}
}

func TestSaveRejectsNonImageWhenValidated(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "wallpaper.jpg")
	if err := os.WriteFile(destination, []byte("valid old wallpaper"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := response(r, http.StatusOK, "<html>error page</html>")
		resp.Header.Set("Content-Type", "text/html")
		return resp, nil
	})}
	_, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/wallpaper.jpg", destination, SaveOptions{ValidateImage: true})
	if err == nil || !strings.Contains(err.Error(), "不是图片") {
		t.Fatalf("error = %v", err)
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "valid old wallpaper" {
		t.Fatalf("destination was replaced: %q", data)
	}
}

func TestSaveAcceptsJPEGPrefix(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "wallpaper.jpg")
	body := "\xff\xd8\xff\xe0jpeg data"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusOK, body), nil
	})}
	result, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/wallpaper.jpg", destination, SaveOptions{ValidateImage: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", result.Size, len(body))
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Fatalf("destination = %q", data)
	}
}

func TestSaveOptionalInvalidAssetIsIgnored(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "accompany.jpg")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, http.StatusOK, "not an image"), nil
	})}
	result, err := (Downloader{HTTP: client}).Save(context.Background(), "https://bing.test/accompany.jpg", destination, SaveOptions{Optional: true, ValidateImage: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != 0 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("invalid optional asset was written: %v", err)
	}
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
