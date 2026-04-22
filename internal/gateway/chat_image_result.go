package gateway

import (
	"strings"
	"time"

	modelpkg "github.com/432539/gpt2api/internal/model"
)

const (
	defaultMixedModeRunTimeout         = 6 * time.Minute
	defaultMixedModePollMaxWait        = 180 * time.Second
	defaultThinkingMixedModeRunTimeout = 10 * time.Minute
	defaultThinkingMixedModePollWait   = 300 * time.Second
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
