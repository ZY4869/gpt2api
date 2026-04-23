package gateway

import (
	"context"
	"testing"
	"time"

	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func TestMixedModeImageStrategyDefaultsThinkingToPictureV2(t *testing.T) {
	h := &Handler{}
	thinkingModel := &modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat}
	plainModel := &modelpkg.Model{Slug: "gpt-5", Type: modelpkg.TypeChat}

	if got := h.mixedModeImageStrategy(thinkingModel); got != chatgpt.ImageStrategyPictureV2Thinking {
		t.Fatalf("thinking strategy = %q, want %q", got, chatgpt.ImageStrategyPictureV2Thinking)
	}
	if got := h.mixedModeImageStrategy(plainModel); got != chatgpt.ImageStrategyPictureV2Thinking {
		t.Fatalf("plain strategy = %q, want %q", got, chatgpt.ImageStrategyPictureV2Thinking)
	}

	h.Settings = fakeSettings{thinkingStrategy: chatgpt.ImageStrategyNativeThinking}
	if got := h.mixedModeImageStrategy(thinkingModel); got != chatgpt.ImageStrategyNativeThinking {
		t.Fatalf("configured strategy = %q, want %q", got, chatgpt.ImageStrategyNativeThinking)
	}
}

type fakeMixedModePoller struct {
	status    chatgpt.PollStatus
	fileIDs   []string
	sediment  []string
	lastConv  string
	lastOpts  chatgpt.PollOpts
	callCount int
}

func (f *fakeMixedModePoller) PollConversationForImages(_ context.Context, convID string, opt chatgpt.PollOpts) (chatgpt.PollStatus, []string, []string) {
	f.lastConv = convID
	f.lastOpts = opt
	f.callCount++
	return f.status, append([]string(nil), f.fileIDs...), append([]string(nil), f.sediment...)
}

func TestResolveMixedModeFileRefsPollsWhenSSEIsPartial(t *testing.T) {
	poller := &fakeMixedModePoller{
		status:  chatgpt.PollStatusIMG2,
		fileIDs: []string{"file_a", "file_b"},
	}
	refs, isPreview, apiErr := resolveMixedModeFileRefs(
		context.Background(),
		poller,
		"conv_partial",
		[]string{"file_a"},
		nil,
		2,
		30*time.Second,
	)
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if isPreview {
		t.Fatal("isPreview = true, want false")
	}
	if poller.callCount != 1 {
		t.Fatalf("poll call count = %d, want 1", poller.callCount)
	}
	if poller.lastConv != "conv_partial" {
		t.Fatalf("poll conv = %q, want conv_partial", poller.lastConv)
	}
	if poller.lastOpts.TargetCount != 2 {
		t.Fatalf("target_count = %d, want 2", poller.lastOpts.TargetCount)
	}
	if len(refs) != 2 || refs[0] != "file_a" || refs[1] != "file_b" {
		t.Fatalf("refs = %#v, want [file_a file_b]", refs)
	}
}

func TestResolveMixedModeFileRefsKeepsPartialImagesWhenPollTimesOut(t *testing.T) {
	poller := &fakeMixedModePoller{status: chatgpt.PollStatusTimeout}
	refs, isPreview, apiErr := resolveMixedModeFileRefs(
		context.Background(),
		poller,
		"conv_partial_timeout",
		[]string{"file_a"},
		nil,
		2,
		30*time.Second,
	)
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if isPreview {
		t.Fatal("isPreview = true, want false")
	}
	if len(refs) != 1 || refs[0] != "file_a" {
		t.Fatalf("refs = %#v, want [file_a]", refs)
	}
}

func TestResolveMixedModeFileRefsFailsWithoutImagesOrConversation(t *testing.T) {
	_, _, apiErr := resolveMixedModeFileRefs(
		context.Background(),
		&fakeMixedModePoller{},
		"",
		nil,
		nil,
		2,
		30*time.Second,
	)
	if apiErr == nil || apiErr.Code != "upstream_image_not_returned" {
		t.Fatalf("apiErr = %+v, want upstream_image_not_returned", apiErr)
	}
}
