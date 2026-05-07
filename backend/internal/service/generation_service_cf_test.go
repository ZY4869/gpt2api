package service

import (
	"context"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
)

func TestRequiresGPTCFWebSolve(t *testing.T) {
	task := &model.GenerationTask{
		Provider:  model.ProviderGPT,
		Kind:      string(provider.KindImage),
		ModelCode: "gpt-image-2",
	}
	if !requiresGPTCFWebSolve(task, map[string]any{"size": "1024x1024"}) {
		t.Fatalf("expected 1K route to require GPT CF solve")
	}
	if requiresGPTCFWebSolve(task, map[string]any{"size": "2480x2480"}) {
		t.Fatalf("expected 4K route to skip GPT CF solve")
	}
}

func TestAttachGPTCFSolverDisabledSkipsSolve(t *testing.T) {
	cfg := &SystemConfigService{
		cache: map[string]string{
			SettingGPTCFEnabled: "false",
		},
	}
	cfg.loaded = time.Now()
	cfg.ttl = 24 * 365 * time.Hour
	svc := &GenerationService{cfg: cfg}
	task := &model.GenerationTask{
		Provider:  model.ProviderGPT,
		Kind:      string(provider.KindImage),
		ModelCode: "gpt-image-2",
	}
	req := &provider.Request{
		Params:   map[string]any{"size": "1024x1024"},
		ProxyURL: "http://proxy.example",
		UpstreamLog: func(ctx context.Context, entry provider.UpstreamLogEntry) {
			if entry.Stage != "cf.solve.start" {
				t.Fatalf("expected cf.solve.start, got %s", entry.Stage)
			}
			if enabled, _ := entry.Meta["enabled"].(bool); enabled {
				t.Fatalf("expected disabled meta")
			}
		},
	}
	if err := svc.attachGPTCFSolver(context.Background(), task, nil, req); err != nil {
		t.Fatalf("attachGPTCFSolver() error = %v", err)
	}
	if req.SolverCookies != "" || req.SolverUserAgent != "" {
		t.Fatalf("expected solver fields to stay empty when disabled")
	}
}
