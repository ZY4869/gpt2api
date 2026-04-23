package chatgpt

import "strings"

const (
	thinkingTriggerReasoningText = "reasoning_text"
	thinkingTriggerThoughtPatch  = "thought_patch"
	thinkingTriggerMetadata      = "metadata"
)

var knownThinkingMetadataKeys = map[string]struct{}{
	"ghostrider_status":    {},
	"reasoning_start_time": {},
	"reasoning_end_time":   {},
	"async_task_title":     {},
}

type thinkingSignalState struct {
	sawThoughtPatch     bool
	sawThinkingMetadata bool
}

func thinkingTriggered(reasoningText string, sawThoughtPatch, sawThinkingMetadata bool) bool {
	return strings.TrimSpace(reasoningText) != "" || sawThoughtPatch || sawThinkingMetadata
}

func thinkingTriggeredVia(reasoningText string, sawThoughtPatch, sawThinkingMetadata bool) string {
	switch {
	case strings.TrimSpace(reasoningText) != "":
		return thinkingTriggerReasoningText
	case sawThoughtPatch:
		return thinkingTriggerThoughtPatch
	case sawThinkingMetadata:
		return thinkingTriggerMetadata
	default:
		return ""
	}
}

func (s *thinkingSignalState) markThoughtPatch() {
	s.sawThoughtPatch = true
}

func (s *thinkingSignalState) observeThinkingMetadata(v interface{}) {
	if hasKnownThinkingMetadata(v) {
		s.sawThinkingMetadata = true
	}
}

func hasKnownThinkingMetadata(v interface{}) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, child := range x {
			if _, ok := knownThinkingMetadataKeys[k]; ok {
				return true
			}
			if hasKnownThinkingMetadata(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range x {
			if hasKnownThinkingMetadata(child) {
				return true
			}
		}
	}
	return false
}
