package gateway

import (
	"strings"
	"time"

	modelpkg "github.com/432539/gpt2api/internal/model"
)

const (
	mixedModeExecStatusCompleted       = "completed"
	mixedModeExecStatusInProgress      = "in_progress"
	defaultMixedModeRunTimeout         = 6 * time.Minute
	defaultMixedModePollMaxWait        = 180 * time.Second
	defaultThinkingMixedModeRunTimeout = 10 * time.Minute
	defaultThinkingMixedModePollWait   = 300 * time.Second
	defaultMixedModeBlockingWait       = 60 * time.Second
	defaultThinkingBlockingWait        = 90 * time.Second
)

func (r *mixedModeExecResult) responseText() string {
	if r == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if text := strings.TrimSpace(r.ReasoningText); text != "" {
		parts = append(parts, text)
	}
	if text := strings.TrimSpace(r.AssistantText); text != "" {
		duplicate := false
		for _, part := range parts {
			if part == text {
				duplicate = true
				break
			}
		}
		if !duplicate {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (r *mixedModeExecResult) readyImageCount() int {
	if r == nil {
		return 0
	}
	if len(r.Images) > 0 {
		return len(r.Images)
	}
	return len(r.FileRefs)
}

func (r *mixedModeExecResult) taskResultURLs() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.Images))
	for _, img := range r.Images {
		if strings.TrimSpace(img.URL) == "" {
			continue
		}
		out = append(out, img.URL)
	}
	return out
}

func (h *Handler) mixedModeRunTimeout(chatModel *modelpkg.Model) time.Duration {
	if isThinkingModel(chatModel) {
		if h.Settings != nil {
			if n := h.Settings.GatewayChatImageThinkingRunTimeoutSec(); n > 0 {
				return time.Duration(n) * time.Second
			}
			if n := h.Settings.GatewayChatImageRunTimeoutSec(); n > 0 {
				return time.Duration(n) * time.Second
			}
		}
		return defaultThinkingMixedModeRunTimeout
	}
	if h.Settings != nil {
		if n := h.Settings.GatewayChatImageRunTimeoutSec(); n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultMixedModeRunTimeout
}

func (h *Handler) mixedModeBlockingWait(chatModel *modelpkg.Model) time.Duration {
	if isThinkingModel(chatModel) {
		return defaultThinkingBlockingWait
	}
	return defaultMixedModeBlockingWait
}

func (h *Handler) mixedModePollMaxWait(chatModel *modelpkg.Model) time.Duration {
	if isThinkingModel(chatModel) {
		if h.Settings != nil {
			if n := h.Settings.GatewayChatImageThinkingPollMaxWaitSec(); n > 0 {
				return time.Duration(n) * time.Second
			}
			if n := h.Settings.GatewayChatImagePollMaxWaitSec(); n > 0 {
				return time.Duration(n) * time.Second
			}
		}
		return defaultThinkingMixedModePollWait
	}
	if h.Settings != nil {
		if n := h.Settings.GatewayChatImagePollMaxWaitSec(); n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultMixedModePollMaxWait
}
