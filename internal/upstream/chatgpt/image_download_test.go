package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestImageDownloadURLUsesPerFileEndpoints(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/files/file_a/download":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"download_url": "https://cdn.example/file_a.png", "status": "ready"})
		case "/backend-api/files/file_b/download":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"download_url": "https://cdn.example/file_b.png", "status": "ready"})
		case "/backend-api/conversation/conv_1/attachment/sed_1/download":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"download_url": "https://cdn.example/sed_1.png", "status": "ready"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cli := &Client{
		opts: Options{
			BaseURL:       srv.URL,
			AuthToken:     "token",
			DeviceID:      "device",
			UserAgent:     DefaultUserAgent,
			ClientVersion: DefaultClientVersion,
			Language:      DefaultLanguage,
		},
		hc: srv.Client(),
	}

	gotA, err := cli.ImageDownloadURL(context.Background(), "conv_1", "file_a")
	if err != nil {
		t.Fatalf("file_a: %v", err)
	}
	gotB, err := cli.ImageDownloadURL(context.Background(), "conv_1", "file_b")
	if err != nil {
		t.Fatalf("file_b: %v", err)
	}
	gotSed, err := cli.ImageDownloadURL(context.Background(), "conv_1", "sed:sed_1")
	if err != nil {
		t.Fatalf("sed: %v", err)
	}

	if gotA != "https://cdn.example/file_a.png" {
		t.Fatalf("file_a download_url = %q", gotA)
	}
	if gotB != "https://cdn.example/file_b.png" {
		t.Fatalf("file_b download_url = %q", gotB)
	}
	if gotSed != "https://cdn.example/sed_1.png" {
		t.Fatalf("sed download_url = %q", gotSed)
	}

	wantPaths := []string{
		"/backend-api/files/file_a/download",
		"/backend-api/files/file_b/download",
		"/backend-api/conversation/conv_1/attachment/sed_1/download",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", paths, wantPaths)
	}
}
