package image

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStoredRefRoundTrip(t *testing.T) {
	raw := EncodeStoredRef(42, "conv_parallel", "sed:sed_123")
	got := ResolveStoredRef(raw, 0, "")

	if got.AccountID != 42 {
		t.Fatalf("account_id = %d, want 42", got.AccountID)
	}
	if got.ConversationID != "conv_parallel" {
		t.Fatalf("conversation_id = %q, want conv_parallel", got.ConversationID)
	}
	if got.Ref != "sed:sed_123" {
		t.Fatalf("ref = %q, want sed:sed_123", got.Ref)
	}
	if PublicFileID(raw) != "sed_123" {
		t.Fatalf("public file id = %q, want sed_123", PublicFileID(raw))
	}
}

func TestResolveStoredRefFallback(t *testing.T) {
	got := ResolveStoredRef("file_abc", 7, "conv_fallback")

	if got.AccountID != 7 {
		t.Fatalf("account_id = %d, want 7", got.AccountID)
	}
	if got.ConversationID != "conv_fallback" {
		t.Fatalf("conversation_id = %q, want conv_fallback", got.ConversationID)
	}
	if got.Ref != "file_abc" {
		t.Fatalf("ref = %q, want file_abc", got.Ref)
	}
}

func TestTaskJSONTags(t *testing.T) {
	startedAt := time.Unix(1710806400, 0).UTC()
	finishedAt := time.Unix(1710806460, 0).UTC()
	payload, err := json.Marshal(Task{
		ID:              1,
		TaskID:          "img_test",
		UserID:          2,
		KeyID:           3,
		ModelID:         4,
		AccountID:       5,
		Prompt:          "draw a cat",
		N:               2,
		Size:            "1024x1024",
		Status:          StatusSuccess,
		ConversationID:  "conv_test",
		FileIDs:         []byte(`["file_1"]`),
		ResultURLs:      []byte(`["https://example.com"]`),
		Error:           "",
		EstimatedCredit: 10,
		CreditCost:      10,
		CreatedAt:       startedAt,
		StartedAt:       &startedAt,
		FinishedAt:      &finishedAt,
	})
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}

	jsonText := string(payload)
	for _, expected := range []string{`"task_id":"img_test"`, `"conversation_id":"conv_test"`, `"credit_cost":10`} {
		if !strings.Contains(jsonText, expected) {
			t.Fatalf("json missing %s: %s", expected, jsonText)
		}
	}
	for _, forbidden := range []string{`"file_ids"`, `"result_urls"`} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("json should not contain %s: %s", forbidden, jsonText)
		}
	}
}
