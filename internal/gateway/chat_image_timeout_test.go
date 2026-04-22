package gateway

import (
	"testing"
	"time"

	modelpkg "github.com/432539/gpt2api/internal/model"
)

func TestMixedModeTimeoutsUseModelAwareDefaults(t *testing.T) {
	h := &Handler{}

	chatModel := &modelpkg.Model{Slug: "gpt-5", Type: modelpkg.TypeChat, Enabled: true}
	thinkingModel := &modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}

	if got := h.mixedModeRunTimeout(chatModel); got != defaultMixedModeRunTimeout {
		t.Fatalf("chat run timeout = %s, want %s", got, defaultMixedModeRunTimeout)
	}
	if got := h.mixedModePollMaxWait(chatModel); got != defaultMixedModePollMaxWait {
		t.Fatalf("chat poll max wait = %s, want %s", got, defaultMixedModePollMaxWait)
	}
	if got := h.mixedModeRunTimeout(thinkingModel); got != defaultThinkingMixedModeRunTimeout {
		t.Fatalf("thinking run timeout = %s, want %s", got, defaultThinkingMixedModeRunTimeout)
	}
	if got := h.mixedModePollMaxWait(thinkingModel); got != defaultThinkingMixedModePollWait {
		t.Fatalf("thinking poll max wait = %s, want %s", got, defaultThinkingMixedModePollWait)
	}
}

func TestMixedModeTimeoutsHonorSettingsOverrides(t *testing.T) {
	h := &Handler{
		Settings: fakeSettings{
			runSec:          410,
			pollSec:         205,
			thinkingRunSec:  730,
			thinkingPollSec: 415,
		},
	}

	chatModel := &modelpkg.Model{Slug: "gpt-5", Type: modelpkg.TypeChat, Enabled: true}
	thinkingModel := &modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}

	if got := h.mixedModeRunTimeout(chatModel); got != 410*time.Second {
		t.Fatalf("chat run timeout = %s, want 410s", got)
	}
	if got := h.mixedModePollMaxWait(chatModel); got != 205*time.Second {
		t.Fatalf("chat poll max wait = %s, want 205s", got)
	}
	if got := h.mixedModeRunTimeout(thinkingModel); got != 730*time.Second {
		t.Fatalf("thinking run timeout = %s, want 730s", got)
	}
	if got := h.mixedModePollMaxWait(thinkingModel); got != 415*time.Second {
		t.Fatalf("thinking poll max wait = %s, want 415s", got)
	}
}

func TestMixedModeThinkingTimeoutsFallbackToSharedOverrides(t *testing.T) {
	h := &Handler{
		Settings: fakeSettings{
			runSec:  520,
			pollSec: 260,
		},
	}

	thinkingModel := &modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}

	if got := h.mixedModeRunTimeout(thinkingModel); got != 520*time.Second {
		t.Fatalf("thinking run timeout = %s, want 520s", got)
	}
	if got := h.mixedModePollMaxWait(thinkingModel); got != 260*time.Second {
		t.Fatalf("thinking poll max wait = %s, want 260s", got)
	}
}
