package model

import "strings"

const (
	StableThinkingSlug          = "gpt-5-thinking"
	DefaultThinkingUpstreamSlug = "gpt-5-4-thinking"
)

var supportedThinkingSlugs = []string{
	StableThinkingSlug,
	DefaultThinkingUpstreamSlug,
	"gpt-5-2-thinking",
}

type ThinkingResolution struct {
	IsThinking           bool
	CanonicalSlug        string
	ResolvedUpstreamSlug string
}

func ResolveThinkingModel(m *Model) ThinkingResolution {
	if m == nil {
		return ThinkingResolution{}
	}

	isThinking := IsThinkingSlug(m.Slug) || IsThinkingSlug(m.UpstreamModelSlug)
	canonical := strings.TrimSpace(m.Slug)
	if canonical == "" && isThinking {
		canonical = StableThinkingSlug
	}

	out := ThinkingResolution{
		IsThinking:    isThinking,
		CanonicalSlug: canonical,
	}
	if !isThinking {
		return out
	}

	resolved := strings.TrimSpace(m.UpstreamModelSlug)
	if resolved == "" {
		resolved = DefaultThinkingUpstreamSlug
	}
	out.ResolvedUpstreamSlug = resolved
	return out
}

func IsThinkingSlug(slug string) bool {
	v := strings.ToLower(strings.TrimSpace(slug))
	if v == "" {
		return false
	}
	return strings.Contains(v, "thinking") || strings.Contains(v, "reasoning") || strings.Contains(v, "-t-")
}

func SupportedThinkingSlugs() []string {
	out := make([]string, len(supportedThinkingSlugs))
	copy(out, supportedThinkingSlugs)
	return out
}
