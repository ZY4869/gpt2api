package chatgpt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetConversationStreamStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/conversation/conv_123/stream_status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"COMPLETE"}`))
	}))
	defer srv.Close()

	cli, err := New(Options{
		BaseURL:   srv.URL,
		AuthToken: "token",
		DeviceID:  "device",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	got, err := cli.GetConversationStreamStatus(context.Background(), "conv_123")
	if err != nil {
		t.Fatalf("GetConversationStreamStatus: %v", err)
	}
	if got != ConversationStreamStatusComplete {
		t.Fatalf("status = %q, want %q", got, ConversationStreamStatusComplete)
	}
}
