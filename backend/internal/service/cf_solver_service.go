package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type CFSolverService struct {
	client *http.Client
}

type CFSolverResult struct {
	Cookies     string
	CFClearance string
	UserAgent   string
	Browser     string
	UpdatedAt   int64
}

func NewCFSolverService() *CFSolverService {
	return &CFSolverService{
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (s *CFSolverService) Solve(ctx context.Context, solverURL, targetURL, proxyURL string, timeout time.Duration) (*CFSolverResult, error) {
	if s == nil {
		return nil, fmt.Errorf("cf solver service is nil")
	}
	solverURL = strings.TrimRight(strings.TrimSpace(solverURL), "/")
	targetURL = strings.TrimSpace(targetURL)
	if solverURL == "" {
		return nil, fmt.Errorf("flaresolverr url is empty")
	}
	if targetURL == "" {
		return nil, fmt.Errorf("cf target url is empty")
	}
	reqBody := map[string]any{
		"cmd":        "request.get",
		"url":        targetURL,
		"maxTimeout": int(timeout / time.Millisecond),
	}
	if proxyURL != "" {
		reqBody["proxy"] = map[string]any{"url": proxyURL}
	}
	rawReq, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, solverURL+"/v1", bytes.NewReader(rawReq))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flaresolverr request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("flaresolverr HTTP %d: %s", resp.StatusCode, snippetString(raw, 300))
	}
	var obj struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			UserAgent string `json:"userAgent"`
			Cookies   []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"cookies"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode flaresolverr: %w", err)
	}
	if !strings.EqualFold(obj.Status, "ok") {
		return nil, fmt.Errorf("flaresolverr status %q: %s", obj.Status, obj.Message)
	}
	parts := make([]string, 0, len(obj.Solution.Cookies))
	cf := ""
	for _, c := range obj.Solution.Cookies {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		value := strings.TrimSpace(c.Value)
		parts = append(parts, name+"="+value)
		if name == "cf_clearance" {
			cf = value
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("flaresolverr returned no cookies")
	}
	ua := strings.TrimSpace(obj.Solution.UserAgent)
	return &CFSolverResult{
		Cookies:     strings.Join(parts, "; "),
		CFClearance: cf,
		UserAgent:   ua,
		Browser:     browserFromUA(ua),
		UpdatedAt:   time.Now().Unix(),
	}, nil
}
