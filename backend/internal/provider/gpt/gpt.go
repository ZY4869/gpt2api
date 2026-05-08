// Package gpt 实现 OpenAI 兼容的图像生成 provider（GPT 账号池 → /v1/images/generations）。
//
// 协议：完全对齐 OpenAI Images API，可对接 OpenAI 官方 / Azure / 任意网关。
//
//	POST {base_url}/v1/images/generations
//	Header: Authorization: Bearer {api_key}
//	Body  : {"model","prompt","n","size","response_format"}
//	Resp  : {"created":int,"data":[{"url":"..."} | {"b64_json":"..."}]}
//
// 错误处理：
//   - 4xx 标记账号失败并 30s 冷却（避免雪崩）；
//   - 5xx 标记账号失败并 5min 冷却；
//   - 超时同上。
package gpt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kleinai/backend/internal/provider"
	"github.com/kleinai/backend/pkg/outbound"
	"golang.org/x/crypto/sha3"
)

const (
	defaultBaseURL               = "https://api.openai.com"
	defaultTimeout               = 6 * time.Minute
	defaultWebImageThinkingModel = "gpt-5-5-thinking"
	webImageWaitAllThenDownload  = "wait_all_then_download"
	webImagePollStepTimeout      = 25 * time.Second
)

// Provider 实现 provider.Provider。
type Provider struct {
	client     *http.Client
	defaultURL string
	name       string
}

// New 构造。defaultBase 为空时使用 OpenAI 官方域名。
func New(defaultBase string) *Provider {
	if defaultBase == "" {
		defaultBase = defaultBaseURL
	}
	return &Provider{
		client: &http.Client{
			Timeout: defaultTimeout,
		},
		defaultURL: strings.TrimRight(defaultBase, "/"),
		name:       "gpt",
	}
}

// Name impl。
func (p *Provider) Name() string { return p.name }

type imgReq struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	Style          string `json:"style,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type imgRespItem struct {
	URL     string `json:"url"`
	B64JSON string `json:"b64_json,omitempty"`
}
type imgResp struct {
	Created int           `json:"created"`
	Data    []imgRespItem `json:"data"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type responseInputItem struct {
	Type     string           `json:"type"`
	Role     string           `json:"role"`
	Content  []map[string]any `json:"content"`
	MetaData map[string]any   `json:"metadata,omitempty"`
}

type responseReq struct {
	Instructions      string           `json:"instructions"`
	Stream            bool             `json:"stream"`
	Reasoning         map[string]any   `json:"reasoning,omitempty"`
	ParallelToolCalls bool             `json:"parallel_tool_calls"`
	Include           []string         `json:"include,omitempty"`
	Model             string           `json:"model"`
	Store             bool             `json:"store"`
	ToolChoice        any              `json:"tool_choice,omitempty"`
	Input             any              `json:"input"`
	Tools             []map[string]any `json:"tools"`
}

type responseCompletedEvent struct {
	Type     string `json:"type"`
	Response struct {
		Output []responseOutputItem `json:"output"`
	} `json:"response"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type responseOutputItem struct {
	Type          string `json:"type"`
	Result        string `json:"result"`
	B64JSON       string `json:"b64_json"`
	ImageB64      string `json:"image_b64"`
	URL           string `json:"url"`
	OutputFormat  string `json:"output_format"`
	Size          string `json:"size"`
	RevisedPrompt string `json:"revised_prompt"`
	Content       []struct {
		Type     string `json:"type"`
		Result   string `json:"result"`
		B64JSON  string `json:"b64_json"`
		ImageB64 string `json:"image_b64"`
		URL      string `json:"url"`
	} `json:"content"`
}

// Generate impl。仅支持 KindImage。
func (p *Provider) Generate(ctx context.Context, req *provider.Request) (*provider.Result, error) {
	if req.Kind != provider.KindImage {
		return nil, fmt.Errorf("gpt provider only supports image kind, got %s", req.Kind)
	}
	if req.Credential == "" {
		return nil, fmt.Errorf("gpt provider missing credential")
	}
	if isGPTImage2(req.ModelCode) {
		if shouldUseWebImage2(req) {
			return p.generateImage2Web(ctx, req)
		}
		return p.generateImage2(ctx, req)
	}

	base := req.BaseURL
	if base == "" {
		base = p.defaultURL
	}
	base = strings.TrimRight(base, "/")
	url := base + "/v1/images/generations"

	count := req.Count
	if count <= 0 {
		count = 1
	}

	body := imgReq{
		Model:          req.ModelCode,
		Prompt:         req.Prompt,
		N:              count,
		Size:           imageSize(req.Params, "1024x1024"),
		Quality:        strParam(req.Params, "quality", ""),
		Style:          strParam(req.Params, "style", ""),
		ResponseFormat: "url",
	}
	payload, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.Credential)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "kleinai/1.0")

	start := time.Now()
	client, err := p.httpClient(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gpt http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gpt %d: %s", resp.StatusCode, snippet(raw, 240))
	}

	var out imgResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gpt decode: %w (raw=%s)", err, snippet(raw, 240))
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, fmt.Errorf("gpt: %s", out.Error.Message)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("gpt returned 0 image")
	}

	width, height := parseSize(body.Size)
	assets := make([]provider.Asset, 0, len(out.Data))
	for _, d := range out.Data {
		a := provider.Asset{
			URL:    d.URL,
			Width:  width,
			Height: height,
			Mime:   "image/png",
		}
		if a.URL == "" && d.B64JSON != "" {
			// 大多数网关会直接给 URL；b64 模式 caller 应自行落 OSS 后再回填。
			a.URL = "data:image/png;base64," + d.B64JSON
		}
		assets = append(assets, a)
	}

	return &provider.Result{
		TaskID:  req.TaskID,
		Assets:  assets,
		Latency: time.Since(start),
	}, nil
}

type webRequirement struct {
	Token      string
	ProofToken string
	SOToken    string
}

type webUploadMeta struct {
	FileID        string
	LibraryFileID string
	FileName      string
	FileSize      int
	Mime          string
	Width         int
	Height        int
}

type webOrderedRef struct {
	FileID     string
	SedimentID string
	RawURL     string
	Source     string
}

type webConversationImageState struct {
	OrderedRefs           []webOrderedRef
	FileIDs               []string
	SedimentIDs           []string
	DirectURLs            []string
	HasAuthoritativeOrder bool
}

type webImageCandidate struct {
	fileID                       string
	sedimentID                   string
	rawURLs                      []string
	contentHash                  string
	authoritativeFinalOrderIndex int
	authoritativeSource          string
	directOrderIndex             int
	fileIDOrderIndex             int
	sedimentIDOrderIndex         int
	firstSeenPollCount           int
	downloadSuccessOrder         int
	dataURL                      string
	mime                         string
	mergedInto                   *webImageCandidate
}

type webImageCandidatePool struct {
	aliases    map[string]*webImageCandidate
	candidates []*webImageCandidate
}

type webImageTestModeState struct {
	Enabled                   bool
	Mode                      string
	DownloadDeferred          bool
	CollectionCandidateCount  int
	AuthoritativeComplete     bool
	AuthoritativeStableRounds int
	FinalDownloadStarted      bool
	FinalDownloadSeqCount     int
	StrictFailOnIncomplete    bool
}

type webImagePollStep struct {
	Name string
	Kind string
}

func (p *Provider) generateImage2Web(ctx context.Context, req *provider.Request) (*provider.Result, error) {
	base := strings.TrimRight(req.BaseURL, "/")
	if base == "" || isCodexBase(base) || strings.Contains(base, "api.openai.com") {
		base = "https://chatgpt.com"
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	size := imageSize(req.Params, "1024x1024")
	ratio := webRatioFromSize(size, strParam(req.Params, "ratio", strParam(req.Params, "aspect_ratio", "1:1")))
	prompt := webImagePromptV2(req.Prompt, ratio, size, count)
	webModel := webImageModelSlug(req)
	conversationLimit, requireCompleteSet := webImageConversationPlan(count)
	testMode := webImageTestMode(req)
	client, err := p.httpClient(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	fp := newWebFP(req.SolverUserAgent)
	start := time.Now()
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider: "gpt",
		Stage:    "web.start",
		Meta: map[string]any{
			"route":                      "chatgpt_web",
			"model":                      webModel,
			"ratio":                      ratio,
			"count":                      count,
			"ref_count":                  len(req.RefAssets),
			"strict_thinking_model":      true,
			"strict_single_conversation": true,
			"conversation_limit":         conversationLimit,
			"require_complete_set":       requireCompleteSet,
		},
	})
	if testMeta := webImageTestModeMeta(testMode); len(testMeta) > 0 {
		logUpstream(ctx, req, provider.UpstreamLogEntry{
			Provider: "gpt",
			Stage:    "web.start",
			Meta:     mergeMeta(map[string]any{}, testMeta),
		})
	}
	if err := p.webBootstrap(ctx, client, base, req.SolverCookies, fp); err != nil {
		logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.bootstrap", Method: "GET", URL: base + "/", Error: err.Error()})
		return nil, err
	}
	reqs, err := p.webRequirements(ctx, client, base, fp, req.Credential, req.SolverCookies)
	if err != nil {
		logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.requirements", Method: "POST", URL: base + "/backend-api/sentinel/chat-requirements", Error: err.Error()})
		return nil, err
	}
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider: "gpt",
		Stage:    "web.requirements",
		Method:   "POST",
		URL:      base + "/backend-api/sentinel/chat-requirements",
		Meta:     map[string]any{"has_token": reqs.Token != "", "has_proof_token": reqs.ProofToken != "", "has_so_token": reqs.SOToken != ""},
	})
	refs := make([]webUploadMeta, 0, len(req.RefAssets))
	for i, ref := range req.RefAssets {
		meta, err := p.webUploadImage(ctx, client, base, fp, req.Credential, req.SolverCookies, strings.TrimSpace(ref), fmt.Sprintf("image_%d.png", i+1))
		if err != nil {
			logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.upload", Method: "POST", URL: base + "/backend-api/files", Error: err.Error(), Meta: map[string]any{"ref_index": i + 1}})
			return nil, err
		}
		logUpstream(ctx, req, provider.UpstreamLogEntry{
			Provider: "gpt",
			Stage:    "web.upload",
			Method:   "POST",
			URL:      base + "/backend-api/files",
			Meta: map[string]any{
				"file_id":   meta.FileID,
				"mime":      meta.Mime,
				"size":      meta.FileSize,
				"width":     meta.Width,
				"height":    meta.Height,
				"ref_index": i + 1,
			},
		})
		refs = append(refs, meta)
	}
	width, height := parseSize(size)
	candidatePool := newWebImageCandidatePool()
	downloadSuccessCounter := 0
	lastDiag := ""
	conduit, err := p.webPrepareImageConversation(ctx, client, base, fp, req.Credential, req.SolverCookies, reqs, prompt, webModel, refs)
	if err != nil {
		logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.prepare", Method: "POST", URL: base + "/backend-api/f/conversation/prepare", Error: err.Error()})
		return nil, err
	}
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider: "gpt",
		Stage:    "web.prepare",
		Method:   "POST",
		URL:      base + "/backend-api/f/conversation/prepare",
		Meta: map[string]any{
			"has_conduit":                conduit != "",
			"strict_single_conversation": true,
			"conversation_limit":         conversationLimit,
		},
	})
	conversationID, fileIDs, sedimentIDs, directURLs, lastText, err := p.webStartImageGeneration(ctx, client, base, fp, req.Credential, req.SolverCookies, reqs, conduit, prompt, webModel, refs)
	if err != nil {
		logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.conversation", Method: "POST", URL: base + "/backend-api/f/conversation", Error: err.Error()})
		return nil, err
	}
	fileIDs, sedimentIDs, directURLs = filterWebGeneratedAssetIDs(fileIDs, sedimentIDs, directURLs, refs)
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider:        "gpt",
		Stage:           "web.conversation",
		Method:          "POST",
		URL:             base + "/backend-api/f/conversation",
		ResponseExcerpt: lastText,
		Meta: map[string]any{
			"conversation_id":            conversationID,
			"file_ids":                   fileIDs,
			"sediment_ids":               sedimentIDs,
			"direct_urls":                len(directURLs),
			"strict_single_conversation": true,
			"conversation_limit":         conversationLimit,
		},
	})
	var urls, downloadErrs []string
	state := webConversationImageState{
		FileIDs:     append([]string(nil), fileIDs...),
		SedimentIDs: append([]string(nil), sedimentIDs...),
		DirectURLs:  append([]string(nil), directURLs...),
	}
	pollWindow := 9 * time.Minute
	if testMode.Enabled {
		pollWindow = 30 * time.Minute
	}
	deadline := webImagePollDeadline(ctx, pollWindow, 15*time.Second)
	pollCount := 0
	snapshot := authoritativeSnapshotKey(state.OrderedRefs, count)
	stableRounds := 0
	for {
		if conversationID != "" {
			pollCtx := webImagePollStepContext(ctx)
			pollState, pollErr := p.webConversationImageIDs(pollCtx, client, base, fp, req.Credential, req.SolverCookies, conversationID, refs)
			webLogPollStep(ctx, req, testMode, webImagePollStep{Name: "web.poll.conversation", Kind: "conversation"}, conversationID, nil, pollState, pollErr)
			pollCount++
			if pollErr == nil {
				addUniqueString(&fileIDs, pollState.FileIDs...)
				addUniqueString(&sedimentIDs, pollState.SedimentIDs...)
				addUniqueString(&directURLs, pollState.DirectURLs...)
				state = mergeWebConversationImageState(state, pollState)
				if pollCount == 1 || pollCount%6 == 0 {
					libCtx := webImagePollStepContext(ctx)
					libFileIDs, libErr := p.webLibraryImageIDs(libCtx, client, base, fp, req.Credential, req.SolverCookies, conversationID, refs)
					webLogPollStep(ctx, req, testMode, webImagePollStep{Name: "web.poll.library", Kind: "library"}, conversationID, map[string]any{"library_file_ids": len(libFileIDs)}, webConversationImageState{}, libErr)
					addUniqueString(&fileIDs, libFileIDs...)
				}
			}
		}
		resolveCtx := webImagePollStepContext(ctx)
		resolvedURLs := p.webResolveImageURLs(resolveCtx, client, base, fp, req.Credential, req.SolverCookies, conversationID, fileIDs, sedimentIDs, refs)
		webLogPollStep(ctx, req, testMode, webImagePollStep{Name: "web.poll.resolve", Kind: "resolve"}, conversationID, map[string]any{"resolved_urls": len(resolvedURLs), "file_ids": len(fileIDs), "sediment_ids": len(sedimentIDs)}, webConversationImageState{}, nil)
		urls = mergeOrderedWebAssetURLs(directURLs, resolvedURLs)
		state.FileIDs = mergeOrderedUniqueStrings(fileIDs)
		state.SedimentIDs = mergeOrderedUniqueStrings(sedimentIDs)
		state.DirectURLs = mergeOrderedWebAssetURLs(directURLs)
		webApplyAuthoritativeOrder(candidatePool, state.OrderedRefs)
		webUpdateCandidatePoolFromResolvedURLs(candidatePool, state, urls, pollCount)
		testMode.CollectionCandidateCount = countWebImageCandidates(candidatePool)
		testMode.AuthoritativeComplete = webAuthoritativeOrderComplete(state, count)
		snapshot, stableRounds = webAuthoritativeStableRounds(state, count, snapshot, stableRounds)
		testMode.AuthoritativeStableRounds = stableRounds
		if pollCount == 1 || pollCount%12 == 0 {
			logUpstream(ctx, req, provider.UpstreamLogEntry{
				Provider:        "gpt",
				Stage:           "web.poll",
				ResponseExcerpt: snippet([]byte(lastText), 160),
				Meta: mergeMeta(map[string]any{
					"poll_count":                 pollCount,
					"conversation_id":            conversationID,
					"file_ids":                   len(fileIDs),
					"sediment_ids":               len(sedimentIDs),
					"direct_urls":                len(directURLs),
					"resolved_urls":              len(urls),
					"download_errors":            len(downloadErrs),
					"authoritative_order_found":  state.HasAuthoritativeOrder,
					"authoritative_count":        len(state.OrderedRefs),
					"strict_single_conversation": true,
				}, webImageTestModeMeta(testMode)),
			})
		}
		for idx, u := range urls {
			candidate := ensureWebImageCandidate(candidatePool, u)
			updateWebImageCandidateOrder(candidate, state, idx, pollCount)
			if candidate.dataURL != "" {
				continue
			}
			if testMode.Enabled && !strings.HasPrefix(strings.TrimSpace(u), "data:") {
				continue
			}
			dataURL, mime, err := p.webDownloadAsDataURL(ctx, client, base, fp, req.Credential, req.SolverCookies, u)
			if err != nil {
				errText := fmt.Sprintf("%s: %v", sanitizeDiagURL(u), err)
				before := len(downloadErrs)
				addUniqueString(&downloadErrs, errText)
				if len(downloadErrs) > before && len(downloadErrs) <= 3 {
					logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.download", Method: "GET", URL: sanitizeDiagURL(u), Error: errText})
				}
				continue
			}
			logUpstream(ctx, req, provider.UpstreamLogEntry{
				Provider: "gpt",
				Stage:    "web.download",
				Method:   "GET",
				URL:      sanitizeDiagURL(u),
				Meta:     map[string]any{"mime": mime, "poll_count": pollCount},
			})
			downloadSuccessCounter++
			candidate.dataURL = dataURL
			candidate.mime = mime
			candidate.downloadSuccessOrder = downloadSuccessCounter
			candidate.contentHash = webImageContentHash(dataURL)
			candidate = mergeWebImageCandidateByContentHash(candidatePool, candidate)
			updateWebImageCandidateOrder(candidate, state, idx, pollCount)
		}
		if testMode.Enabled {
			if (testMode.CollectionCandidateCount >= count && testMode.AuthoritativeComplete && stableRounds >= 2) || conversationID == "" || time.Now().After(deadline) {
				break
			}
		} else if countWebImageCandidatesWithData(candidatePool) >= count || conversationID == "" || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			lastDiag = webImage2Diag(conversationID, fileIDs, sedimentIDs, directURLs, urls, downloadErrs, lastText)
			logUpstream(ctx, req, provider.UpstreamLogEntry{
				Provider:        "gpt",
				Stage:           "web.wait_timeout",
				ResponseExcerpt: lastDiag,
				Error:           ctx.Err().Error(),
				Meta: mergeMeta(map[string]any{
					"poll_count":                 pollCount,
					"asset_count":                countWebImageCandidatesWithData(candidatePool),
					"resolved_urls":              len(urls),
					"download_errors":            downloadErrs,
					"authoritative_order_found":  state.HasAuthoritativeOrder,
					"authoritative_count":        len(state.OrderedRefs),
					"strict_single_conversation": true,
				}, webImageTestModeMeta(testMode)),
			})
			return nil, fmt.Errorf("gpt image2 web wait: %w", ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
	settleMeta := map[string]any{
		"settle_started":            false,
		"settle_completed":          false,
		"settle_poll_count":         0,
		"authoritative_order_found": state.HasAuthoritativeOrder,
		"authoritative_count":       len(state.OrderedRefs),
		"fallback_used":             false,
	}
	if !testMode.Enabled && conversationID != "" && count > 1 && countWebImageCandidatesWithData(candidatePool) >= count {
		settleMeta["settle_started"] = true
		settledState, settlePollCount, settled := webSettleImageOrder(ctx, p, client, base, fp, req.Credential, req.SolverCookies, conversationID, refs, state, count, deadline)
		settleMeta["settle_poll_count"] = settlePollCount
		settleMeta["settle_completed"] = settled
		settleMeta["authoritative_order_found"] = settledState.HasAuthoritativeOrder
		settleMeta["authoritative_count"] = len(settledState.OrderedRefs)
		settleMeta["fallback_used"] = !settledState.HasAuthoritativeOrder || len(settledState.OrderedRefs) < count
		state = settledState
		webApplyAuthoritativeOrder(candidatePool, state.OrderedRefs)
	}
	if testMode.Enabled {
		settleMeta["settle_started"] = true
		settleMeta["settle_completed"] = testMode.AuthoritativeComplete && stableRounds >= 2
		settleMeta["settle_poll_count"] = pollCount
		settleMeta["fallback_used"] = !settleMeta["settle_completed"].(bool)
	}
	lastDiag = webImage2Diag(conversationID, fileIDs, sedimentIDs, directURLs, urls, downloadErrs, lastText)
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider:        "gpt",
		Stage:           "web.resolve",
		ResponseExcerpt: lastDiag,
		Meta: mergeMeta(map[string]any{
			"poll_count":                 pollCount,
			"resolved_urls":              len(urls),
			"download_errors":            downloadErrs,
			"asset_count":                countWebImageCandidatesWithData(candidatePool),
			"authoritative_order_found":  state.HasAuthoritativeOrder,
			"authoritative_count":        len(state.OrderedRefs),
			"settle_started":             settleMeta["settle_started"],
			"settle_completed":           settleMeta["settle_completed"],
			"settle_poll_count":          settleMeta["settle_poll_count"],
			"fallback_used":              settleMeta["fallback_used"],
			"strict_single_conversation": true,
		}, webImageTestModeMeta(testMode)),
	})
	if countWebImageCandidatesWithData(candidatePool) == 0 && conversationID == "" && lastText != "" {
		return nil, fmt.Errorf("gpt image2 web produced text instead of image: %s", snippet([]byte(lastText), 220))
	}
	if testMode.Enabled {
		if testMode.CollectionCandidateCount < count || !testMode.AuthoritativeComplete || stableRounds < 2 {
			errText := fmt.Sprintf("gpt image2 web test mode did not reach complete final set (%d/%d authoritative=%v stable_rounds=%d)", testMode.CollectionCandidateCount, count, testMode.AuthoritativeComplete, stableRounds)
			logUpstream(ctx, req, provider.UpstreamLogEntry{
				Provider:        "gpt",
				Stage:           "web.failed",
				ResponseExcerpt: lastDiag,
				Error:           errText,
				Meta:            mergeMeta(map[string]any{"conversation_id": conversationID}, webImageTestModeMeta(testMode)),
			})
			return nil, fmt.Errorf("%s", errText)
		}
		finalCandidates := buildFinalOrderedWebCandidates(candidatePool, count)
		testMode.FinalDownloadStarted = true
		testMode.FinalDownloadSeqCount = len(finalCandidates)
		for seq, candidate := range finalCandidates {
			candidate = canonicalWebImageCandidate(candidate)
			if candidate == nil {
				continue
			}
			if candidate.dataURL != "" {
				continue
			}
			downloadURL := firstWebImageDownloadURL(candidate)
			if downloadURL == "" {
				continue
			}
			dataURL, mime, err := p.webDownloadAsDataURL(ctx, client, base, fp, req.Credential, req.SolverCookies, downloadURL)
			if err != nil {
				errText := fmt.Sprintf("%s: %v", sanitizeDiagURL(downloadURL), err)
				logUpstream(ctx, req, provider.UpstreamLogEntry{
					Provider: "gpt",
					Stage:    "web.download",
					Method:   "GET",
					URL:      sanitizeDiagURL(downloadURL),
					Error:    errText,
					Meta:     mergeMeta(map[string]any{"final_download_seq": seq}, webImageTestModeMeta(testMode)),
				})
				return nil, fmt.Errorf("gpt image2 web test mode final download failed: %s", errText)
			}
			downloadSuccessCounter++
			candidate.dataURL = dataURL
			candidate.mime = mime
			candidate.downloadSuccessOrder = downloadSuccessCounter
			candidate.contentHash = webImageContentHash(dataURL)
			logUpstream(ctx, req, provider.UpstreamLogEntry{
				Provider: "gpt",
				Stage:    "web.download",
				Method:   "GET",
				URL:      sanitizeDiagURL(downloadURL),
				Meta:     mergeMeta(map[string]any{"mime": mime, "final_download_seq": seq}, webImageTestModeMeta(testMode)),
			})
		}
		if countWebImageCandidatesWithData(candidatePool) < count {
			errText := fmt.Sprintf("gpt image2 web test mode final download returned %d/%d images", countWebImageCandidatesWithData(candidatePool), count)
			logUpstream(ctx, req, provider.UpstreamLogEntry{
				Provider:        "gpt",
				Stage:           "web.failed",
				ResponseExcerpt: lastDiag,
				Error:           errText,
				Meta:            mergeMeta(map[string]any{"conversation_id": conversationID}, webImageTestModeMeta(testMode)),
			})
			return nil, fmt.Errorf("%s", errText)
		}
	}
	assets := buildOrderedWebAssets(candidatePool, count, width, height, ratio)
	if len(assets) == 0 {
		if lastDiag != "" {
			logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.failed", ResponseExcerpt: lastDiag})
			return nil, fmt.Errorf("gpt image2 web returned 0 image (%s)", lastDiag)
		}
		logUpstream(ctx, req, provider.UpstreamLogEntry{Provider: "gpt", Stage: "web.failed", ResponseExcerpt: "gpt image2 web returned 0 image"})
		return nil, fmt.Errorf("gpt image2 web returned 0 image")
	}
	if requireCompleteSet && len(assets) < count {
		errText := fmt.Sprintf("gpt image2 web single conversation returned %d/%d images", len(assets), count)
		if lastDiag != "" {
			errText += " (" + lastDiag + ")"
		}
		logUpstream(ctx, req, provider.UpstreamLogEntry{
			Provider:        "gpt",
			Stage:           "web.failed",
			ResponseExcerpt: lastDiag,
			Error:           errText,
			Meta: map[string]any{
				"conversation_id":            conversationID,
				"asset_count":                len(assets),
				"requested_count":            count,
				"strict_single_conversation": true,
				"conversation_limit":         conversationLimit,
			},
		})
		return nil, fmt.Errorf("%s", errText)
	}
	return &provider.Result{TaskID: req.TaskID, Assets: assets, Latency: time.Since(start)}, nil
}

func appendUniqueWebAsset(assets []provider.Asset, seen map[string]bool, asset provider.Asset) ([]provider.Asset, bool) {
	key := strings.TrimSpace(asset.URL)
	if key == "" {
		return assets, false
	}
	if seen[key] {
		return assets, false
	}
	seen[key] = true
	return append(assets, asset), true
}

func webImagePollDeadline(ctx context.Context, maxWindow, safetyMargin time.Duration) time.Time {
	deadline := time.Now().Add(maxWindow)
	if dl, ok := ctx.Deadline(); ok {
		safeDeadline := dl.Add(-safetyMargin)
		if safeDeadline.Before(deadline) {
			deadline = safeDeadline
		}
	}
	return deadline
}

func (p *Provider) generateImage2(ctx context.Context, req *provider.Request) (*provider.Result, error) {
	base := strings.TrimRight(req.BaseURL, "/")
	if base == "" {
		base = p.defaultURL
	}
	url := responseEndpoint(base)
	count := req.Count
	if count <= 0 {
		count = 1
	}
	modelCode := req.ModelCode
	mainModel := strParam(req.Params, "main_model", mainModelForImage2(modelCode))
	toolModel := imageToolModel(modelCode)
	size := imageSize(req.Params, "1024x1024")
	action := "generate"
	if req.Mode == provider.ModeI2I || len(req.RefAssets) > 0 || strings.EqualFold(strParam(req.Params, "operation", ""), "edit") {
		action = "edit"
	}
	content := []map[string]any{{"type": "input_text", "text": req.Prompt}}
	for _, ref := range req.RefAssets {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		content = append(content, map[string]any{"type": "input_image", "image_url": ref})
	}
	input := []responseInputItem{{Type: "message", Role: "user", Content: content}}
	tool := map[string]any{
		"type":   "image_generation",
		"action": action,
		"model":  toolModel,
		"size":   size,
	}
	if quality := imageQuality(req.Params); quality != "" {
		tool["quality"] = quality
	}
	copyParam(tool, req.Params, "background")
	copyParam(tool, req.Params, "output_format")
	copyParam(tool, req.Params, "output_compression")
	copyParam(tool, req.Params, "partial_images")
	copyParam(tool, req.Params, "moderation")
	copyParam(tool, req.Params, "input_fidelity")
	if mask := firstStringParam(req.Params, "mask", "mask_image_url"); mask != "" {
		tool["input_image_mask"] = map[string]string{"image_url": mask}
	}
	body := responseReq{
		Instructions:      "You are an image generation assistant. Follow the user's prompt and return the generated image.",
		Stream:            true,
		Reasoning:         map[string]any{"effort": "medium", "summary": "auto"},
		ParallelToolCalls: true,
		Include:           []string{"reasoning.encrypted_content"},
		Model:             mainModel,
		Store:             false,
		ToolChoice:        "auto",
		Input:             input,
		Tools:             []map[string]any{tool},
	}

	start := time.Now()
	client, err := p.httpClient(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	width, height := parseSize(size)
	assets := make([]provider.Asset, 0, count)
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider: "gpt",
		Stage:    "codex.start",
		Method:   "POST",
		URL:      url,
		Meta: map[string]any{
			"model":          modelCode,
			"main_model":     mainModel,
			"tool_model":     toolModel,
			"size":           size,
			"count":          count,
			"action":         action,
			"ref_count":      len(req.RefAssets),
			"proxy":          req.ProxyURL != "",
			"has_toolchoice": true,
		},
	})
	for i := 0; i < count && len(assets) < count; i++ {
		attemptBody := body
		retriedWithoutToolChoice := false
		for {
			payload, _ := json.Marshal(attemptBody)
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("Authorization", "Bearer "+req.Credential)
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			httpReq.Header.Set("User-Agent", userAgentForEndpoint(url))
			if isCodexEndpoint(url) {
				httpReq.Header.Set("Originator", "codex-tui")
				httpReq.Header.Set("Connection", "Keep-Alive")
			}
			resp, err := client.Do(httpReq)
			if err != nil {
				logUpstream(ctx, req, provider.UpstreamLogEntry{
					Provider:       "gpt",
					Stage:          "codex.request",
					Method:         "POST",
					URL:            url,
					RequestExcerpt: snippet(payload, 600),
					Error:          err.Error(),
					Meta: map[string]any{
						"model":      modelCode,
						"size":       size,
						"count":      count,
						"tool_model": toolModel,
						"action":     action,
					},
				})
				return nil, fmt.Errorf("gpt image2 http: %w", err)
			}
			if resp.StatusCode >= 400 {
				raw, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if retriedWithoutToolChoice {
					logUpstream(ctx, req, provider.UpstreamLogEntry{
						Provider:        "gpt",
						Stage:           "codex.response",
						Method:          "POST",
						URL:             url,
						StatusCode:      resp.StatusCode,
						RequestExcerpt:  snippet(payload, 600),
						ResponseExcerpt: snippet(raw, 600),
						Meta: map[string]any{
							"model":      modelCode,
							"size":       size,
							"count":      count,
							"tool_model": toolModel,
							"action":     action,
						},
					})
				}
				if !retriedWithoutToolChoice && shouldRetryImage2WithoutToolChoice(raw) {
					logUpstream(ctx, req, provider.UpstreamLogEntry{
						Provider:        "gpt",
						Stage:           "codex.retry",
						Method:          "POST",
						URL:             url,
						StatusCode:      resp.StatusCode,
						RequestExcerpt:  snippet(payload, 600),
						ResponseExcerpt: snippet(raw, 600),
						Meta: map[string]any{
							"reason": "tool_choice_fallback",
						},
					})
					attemptBody.ToolChoice = nil
					retriedWithoutToolChoice = true
					continue
				}
				logUpstream(ctx, req, provider.UpstreamLogEntry{
					Provider:        "gpt",
					Stage:           "codex.failed",
					Method:          "POST",
					URL:             url,
					StatusCode:      resp.StatusCode,
					RequestExcerpt:  snippet(payload, 600),
					ResponseExcerpt: snippet(raw, 600),
					Meta: map[string]any{
						"model":      modelCode,
						"size":       size,
						"count":      count,
						"tool_model": toolModel,
						"action":     action,
					},
				})
				return nil, fmt.Errorf("gpt image2 %d: %s", resp.StatusCode, snippet(raw, 320))
			}
			completed, err := parseCompletedResponse(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				logUpstream(ctx, req, provider.UpstreamLogEntry{
					Provider:       "gpt",
					Stage:          "codex.decode",
					Method:         "POST",
					URL:            url,
					RequestExcerpt: snippet(payload, 600),
					Error:          err.Error(),
					Meta: map[string]any{
						"model":      modelCode,
						"size":       size,
						"count":      count,
						"tool_model": toolModel,
						"action":     action,
					},
				})
				return nil, err
			}
			if completed.Error != nil && completed.Error.Message != "" {
				logUpstream(ctx, req, provider.UpstreamLogEntry{
					Provider:        "gpt",
					Stage:           "codex.failed",
					Method:          "POST",
					URL:             url,
					RequestExcerpt:  snippet(payload, 600),
					ResponseExcerpt: completed.Error.Message,
					Meta: map[string]any{
						"model":      modelCode,
						"size":       size,
						"count":      count,
						"tool_model": toolModel,
						"action":     action,
					},
				})
				return nil, fmt.Errorf("gpt image2: %s", completed.Error.Message)
			}
			for _, out := range completed.Response.Output {
				imageData, imageURL := outputImagePayload(out)
				if out.Type != "image_generation_call" && imageData == "" && imageURL == "" {
					continue
				}
				mime := mimeForImageFormat(out.OutputFormat)
				assetWidth, assetHeight := width, height
				if out.Size != "" {
					assetWidth, assetHeight = parseSize(out.Size)
				}
				assetURL := imageURL
				if assetURL == "" {
					assetURL = "data:" + mime + ";base64," + imageData
				}
				assets = append(assets, provider.Asset{
					URL:    assetURL,
					Width:  assetWidth,
					Height: assetHeight,
					Mime:   mime,
					Meta:   map[string]any{"revised_prompt": out.RevisedPrompt, "provider_action": action, "size": size},
				})
				logUpstream(ctx, req, provider.UpstreamLogEntry{
					Provider:        "gpt",
					Stage:           "codex.asset",
					Method:          "POST",
					URL:             url,
					RequestExcerpt:  snippet(payload, 600),
					ResponseExcerpt: assetURL,
					Meta: map[string]any{
						"model":       modelCode,
						"size":        size,
						"count":       count,
						"tool_model":  toolModel,
						"action":      action,
						"asset_index": len(assets),
					},
				})
				if len(assets) >= count {
					break
				}
			}
			break
		}
	}
	if len(assets) == 0 {
		logUpstream(ctx, req, provider.UpstreamLogEntry{
			Provider:        "gpt",
			Stage:           "codex.failed",
			Method:          "POST",
			URL:             url,
			ResponseExcerpt: "gpt image2 returned 0 image",
			Meta: map[string]any{
				"model":      modelCode,
				"size":       size,
				"count":      count,
				"tool_model": toolModel,
				"action":     action,
			},
		})
		return nil, fmt.Errorf("gpt image2 returned 0 image")
	}
	logUpstream(ctx, req, provider.UpstreamLogEntry{
		Provider: "gpt",
		Stage:    "codex.success",
		Method:   "POST",
		URL:      url,
		Meta: map[string]any{
			"model":      modelCode,
			"size":       size,
			"count":      count,
			"tool_model": toolModel,
			"action":     action,
			"assets":     len(assets),
		},
	})
	return &provider.Result{TaskID: req.TaskID, Assets: assets, Latency: time.Since(start)}, nil
}

type webFP struct {
	UserAgent     string
	DeviceID      string
	SessionID     string
	ClientVersion string
	BuildNumber   string
	SecCHUA       string
	Platform      string
}

func newWebFP(userAgent string) webFP {
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"
	}
	secCHUA := webSecCHUAFromUA(userAgent)
	if secCHUA == "" {
		secCHUA = `"Microsoft Edge";v="143", "Chromium";v="143", "Not A(Brand";v="24"`
	}
	return webFP{
		UserAgent:     userAgent,
		DeviceID:      uuid.NewString(),
		SessionID:     uuid.NewString(),
		ClientVersion: "prod-be885abbfcfe7b1f511e88b3003d9ee44757fbad",
		BuildNumber:   "5955942",
		SecCHUA:       secCHUA,
		Platform:      webPlatformFromUA(userAgent),
	}
}

func webSecCHUAFromUA(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return ""
	}
	switch {
	case strings.Contains(ua, "Edg/"):
		version := firstBrowserMajor(ua, `Edg/(\d+)`)
		if version == "" {
			version = "143"
		}
		return fmt.Sprintf(`"Microsoft Edge";v="%s", "Chromium";v="%s", "Not_A Brand";v="24"`, version, version)
	case strings.Contains(ua, "Chrome/"):
		version := firstBrowserMajor(ua, `Chrome/(\d+)`)
		if version == "" {
			version = "143"
		}
		return fmt.Sprintf(`"Google Chrome";v="%s", "Chromium";v="%s", "Not_A Brand";v="24"`, version, version)
	case strings.Contains(ua, "Firefox/"):
		version := firstBrowserMajor(ua, `Firefox/(\d+)`)
		if version == "" {
			version = "138"
		}
		return fmt.Sprintf(`"Firefox";v="%s", "Not_A Brand";v="24"`, version)
	default:
		return ""
	}
}

func webPlatformFromUA(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "mac os x"):
		return "macOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return "Windows"
	}
}

func firstBrowserMajor(userAgent, pattern string) string {
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(userAgent)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func (p *Provider) webBootstrap(ctx context.Context, client *http.Client, base, cookie string, fp webFP) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if err != nil {
		return err
	}
	for k, v := range webBootstrapHeaders(fp, cookie) {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gpt image2 web bootstrap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 320))
		return fmt.Errorf("gpt image2 web bootstrap %d: %s", resp.StatusCode, string(raw))
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 512*1024))
	return nil
}

func (p *Provider) webRequirements(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie string) (webRequirement, error) {
	path := "/backend-api/sentinel/chat-requirements"
	body := map[string]string{"p": buildLegacyRequirementsToken(fp.UserAgent)}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return webRequirement{}, err
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return webRequirement{}, fmt.Errorf("gpt image2 web requirements: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return webRequirement{}, fmt.Errorf("gpt image2 web requirements %d: %s", resp.StatusCode, snippet(raw, 320))
	}
	var out struct {
		Token       string `json:"token"`
		SOToken     string `json:"so_token"`
		ProofOfWork struct {
			Required   bool   `json:"required"`
			Seed       string `json:"seed"`
			Difficulty string `json:"difficulty"`
		} `json:"proofofwork"`
		Arkose struct {
			Required bool `json:"required"`
		} `json:"arkose"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return webRequirement{}, fmt.Errorf("gpt image2 web requirements decode: %w", err)
	}
	if out.Arkose.Required {
		return webRequirement{}, fmt.Errorf("gpt image2 web requires arkose")
	}
	if out.Token == "" {
		return webRequirement{}, fmt.Errorf("gpt image2 web requirements missing token")
	}
	proof := ""
	if out.ProofOfWork.Required && out.ProofOfWork.Seed != "" && out.ProofOfWork.Difficulty != "" {
		proof = buildProofToken(out.ProofOfWork.Seed, out.ProofOfWork.Difficulty, fp.UserAgent)
	}
	return webRequirement{Token: out.Token, ProofToken: proof, SOToken: out.SOToken}, nil
}

func (p *Provider) webPrepareImageConversation(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie string, reqs webRequirement, prompt, modelSlug string, refs []webUploadMeta) (string, error) {
	path := "/backend-api/f/conversation/prepare"
	body := map[string]any{
		"action":                 "next",
		"fork_from_shared_post":  false,
		"parent_message_id":      "client-created-root",
		"model":                  modelSlug,
		"client_prepare_state":   "none",
		"timezone_offset_min":    -480,
		"timezone":               "Asia/Shanghai",
		"conversation_mode":      map[string]any{"kind": "primary_assistant"},
		"system_hints":           []string{"picture_v2"},
		"attachment_mime_types":  []string{"image/png"},
		"supports_buffering":     true,
		"supported_encodings":    []string{"v1"},
		"client_contextual_info": map[string]any{"app_name": "chatgpt.com"},
		"thinking_effort":        "standard",
	}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	for k, v := range webImageHeaders(fp, token, path, cookie, reqs, "", "*/*") {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gpt image2 web prepare: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gpt image2 web prepare %d: %s", resp.StatusCode, snippet(raw, 320))
	}
	var out struct {
		ConduitToken string `json:"conduit_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("gpt image2 web prepare decode: %w", err)
	}
	if out.ConduitToken == "" {
		return "", fmt.Errorf("gpt image2 web prepare missing conduit token")
	}
	return out.ConduitToken, nil
}

func (p *Provider) webStartImageGeneration(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie string, reqs webRequirement, conduit, prompt, modelSlug string, refs []webUploadMeta) (string, []string, []string, []string, string, error) {
	path := "/backend-api/f/conversation"
	content, metadata := webImageMessageContent(prompt, refs)
	messageID := uuid.NewString()
	body := map[string]any{
		"action":                   "next",
		"fork_from_shared_post":    false,
		"parent_message_id":        "client-created-root",
		"model":                    modelSlug,
		"client_prepare_state":     "success",
		"timezone_offset_min":      -480,
		"timezone":                 "Asia/Shanghai",
		"conversation_mode":        map[string]any{"kind": "primary_assistant"},
		"enable_message_followups": true,
		"system_hints":             []string{},
		"supports_buffering":       true,
		"supported_encodings":      []string{"v1"},
		"client_contextual_info": map[string]any{
			"is_dark_mode": false, "time_since_loaded": 51, "page_height": 1111, "page_width": 1731,
			"pixel_ratio": 1.5, "screen_height": 1440, "screen_width": 2560, "app_name": "chatgpt.com",
		},
		"paragen_cot_summary_display_override": "allow",
		"force_parallel_switch":                "auto",
		"thinking_effort":                      "standard",
		"messages": []map[string]any{{
			"id":          messageID,
			"author":      map[string]string{"role": "user"},
			"create_time": time.Now().Unix(),
			"content":     content,
			"metadata":    metadata,
		}},
	}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return "", nil, nil, nil, "", err
	}
	for k, v := range webImageHeaders(fp, token, path, cookie, reqs, conduit, "text/event-stream") {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", nil, nil, nil, "", fmt.Errorf("gpt image2 web conversation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", nil, nil, nil, "", fmt.Errorf("gpt image2 web conversation %d: %s", resp.StatusCode, snippet(raw, 320))
	}
	conversationID, fileIDs, sedimentIDs, directURLs, lastText, err := parseWebImageSSE(resp.Body)
	if err != nil {
		return "", nil, nil, nil, "", err
	}
	return conversationID, fileIDs, sedimentIDs, directURLs, lastText, nil
}

func webImageMessageContent(prompt string, refs []webUploadMeta) (map[string]any, map[string]any) {
	parts := make([]any, 0, len(refs)+1)
	attachments := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, map[string]any{
			"content_type":  "image_asset_pointer",
			"asset_pointer": "sediment://file_" + strings.TrimPrefix(ref.FileID, "file_"),
			"width":         ref.Width,
			"height":        ref.Height,
			"size_bytes":    ref.FileSize,
		})
		attachment := map[string]any{
			"id":           ref.FileID,
			"mime_type":    ref.Mime,
			"name":         ref.FileName,
			"size":         ref.FileSize,
			"width":        ref.Width,
			"height":       ref.Height,
			"source":       "library",
			"is_big_paste": false,
		}
		if ref.LibraryFileID != "" {
			attachment["library_file_id"] = ref.LibraryFileID
		}
		attachments = append(attachments, attachment)
	}
	if len(refs) > 0 {
		parts = append(parts, prompt)
	}
	content := map[string]any{"content_type": "text", "parts": []string{prompt}}
	if len(refs) > 0 {
		content = map[string]any{"content_type": "multimodal_text", "parts": parts}
	}
	metadata := map[string]any{
		"developer_mode_connector_ids": []string{},
		"selected_github_repos":        []string{},
		"selected_all_github_repos":    false,
		"system_hints":                 []string{"picture_v2"},
		"serialization_metadata":       map[string]any{"custom_symbol_offsets": []any{}},
	}
	if len(attachments) > 0 {
		metadata["attachments"] = attachments
	}
	return content, metadata
}

func (p *Provider) webPollImageResults(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, conversationID string, timeout time.Duration, refs []webUploadMeta) (webConversationImageState, error) {
	if conversationID == "" {
		return webConversationImageState{}, nil
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		state, err := p.webConversationImageIDs(ctx, client, base, fp, token, cookie, conversationID, refs)
		if err == nil && (len(state.FileIDs) > 0 || len(state.SedimentIDs) > 0 || len(state.DirectURLs) > 0 || len(state.OrderedRefs) > 0) {
			return state, nil
		}
		lastErr = err
		time.Sleep(4 * time.Second)
	}
	return webConversationImageState{}, lastErr
}

func (p *Provider) webConversationImageIDs(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, conversationID string, refs []webUploadMeta) (webConversationImageState, error) {
	path := "/backend-api/conversation/" + conversationID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return webConversationImageState{}, err
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Accept", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return webConversationImageState{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return webConversationImageState{}, fmt.Errorf("gpt image2 web poll %d: %s", resp.StatusCode, snippet(raw, 240))
	}
	toolFileIDs, toolSedimentIDs := extractWebImageToolIDs(raw)
	_, rawFileIDs, rawSedimentIDs, directURLs := extractWebImageIDs(string(raw))
	fileIDs := mergeOrderedUniqueStrings(rawFileIDs, toolFileIDs)
	sedimentIDs := mergeOrderedUniqueStrings(rawSedimentIDs, toolSedimentIDs)
	fileIDs, sedimentIDs, directURLs = filterWebGeneratedAssetIDs(fileIDs, sedimentIDs, directURLs, refs)
	orderedRefs, hasAuthoritative := extractWebAuthoritativeOrderedRefs(raw, refs)
	return webConversationImageState{
		OrderedRefs:           orderedRefs,
		FileIDs:               fileIDs,
		SedimentIDs:           sedimentIDs,
		DirectURLs:            directURLs,
		HasAuthoritativeOrder: hasAuthoritative,
	}, nil
}

func (p *Provider) webLibraryImageIDs(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, conversationID string, refs []webUploadMeta) ([]string, error) {
	path := "/backend-api/files/library"
	body := map[string]any{"limit": 20, "cursor": nil}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gpt image2 web library %d: %s", resp.StatusCode, snippet(raw, 240))
	}
	var out struct {
		Items []struct {
			FileID               string `json:"file_id"`
			MimeType             string `json:"mime_type"`
			LibraryFileCategory  string `json:"library_file_category"`
			State                string `json:"state"`
			OriginationThreadID  string `json:"origination_thread_id"`
			OriginationMessageID string `json:"origination_message_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var ids []string
	for _, item := range out.Items {
		if item.FileID == "" || item.OriginationThreadID != conversationID {
			continue
		}
		if item.State != "" && !strings.EqualFold(item.State, "ready") {
			continue
		}
		if item.LibraryFileCategory != "" && !strings.EqualFold(item.LibraryFileCategory, "image") {
			continue
		}
		if item.MimeType != "" && !strings.HasPrefix(strings.ToLower(item.MimeType), "image/") {
			continue
		}
		addUniqueString(&ids, item.FileID)
	}
	ids, _, _ = filterWebGeneratedAssetIDs(ids, nil, nil, refs)
	return ids, nil
}

func (p *Provider) webResolveImageURLs(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, conversationID string, fileIDs, sedimentIDs []string, refs []webUploadMeta) []string {
	var out []string
	seen := map[string]bool{}
	exclude := map[string]bool{}
	for _, ref := range refs {
		if ref.FileID != "" {
			exclude[ref.FileID] = true
		}
		if ref.LibraryFileID != "" {
			exclude[ref.LibraryFileID] = true
		}
	}
	for _, id := range fileIDs {
		if id == "" || id == "file_upload" || seen["file:"+id] || exclude[id] {
			continue
		}
		seen["file:"+id] = true
		path := "/backend-api/files/download/" + id
		if conversationID != "" {
			path += "?conversation_id=" + url.QueryEscape(conversationID) + "&inline=false"
		}
		if u := p.webDownloadURL(ctx, client, base, fp, token, cookie, path); u != "" {
			out = append(out, u)
		}
	}
	if conversationID == "" {
		return out
	}
	for _, id := range sedimentIDs {
		if id == "" || seen["sed:"+id] || exclude[id] {
			continue
		}
		seen["sed:"+id] = true
		if u := p.webDownloadURL(ctx, client, base, fp, token, cookie, "/backend-api/conversation/"+conversationID+"/attachment/"+id+"/download"); u != "" {
			out = append(out, u)
		}
	}
	return out
}

func filterWebGeneratedAssetIDs(fileIDs, sedimentIDs, directURLs []string, refs []webUploadMeta) ([]string, []string, []string) {
	exclude := map[string]bool{}
	for _, ref := range refs {
		if ref.FileID != "" {
			exclude[ref.FileID] = true
		}
		if ref.LibraryFileID != "" {
			exclude[ref.LibraryFileID] = true
		}
	}
	filter := func(in []string) []string {
		out := make([]string, 0, len(in))
		seen := map[string]bool{}
		for _, v := range in {
			if v == "" || exclude[v] || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
		return out
	}
	return filter(fileIDs), filter(sedimentIDs), filter(directURLs)
}

func (p *Provider) webDownloadURL(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, path string) string {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return ""
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Accept", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return ""
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return ""
	}
	for _, k := range []string{"download_url", "url"} {
		if s, ok := out[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func (p *Provider) webDownloadAsDataURL(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, rawURL string) (string, string, error) {
	downloadURL := rawURL
	if strings.HasPrefix(downloadURL, "/") {
		downloadURL = strings.TrimRight(base, "/") + downloadURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", "", err
	}
	if shouldUseWebDownloadHeaders(base, downloadURL) {
		targetPath := "/"
		if parsed, err := url.Parse(downloadURL); err == nil && parsed.Path != "" {
			targetPath = parsed.Path
		}
		for k, v := range webBaseHeaders(fp, token, targetPath, cookie) {
			httpReq.Header.Set(k, v)
		}
		httpReq.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("download image %d: %s", resp.StatusCode, snippet(data, 160))
	}
	if len(data) == 0 {
		return "", "", fmt.Errorf("download image empty body")
	}
	mime := resp.Header.Get("Content-Type")
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = mime[:idx]
	}
	if mime == "" || !strings.HasPrefix(mime, "image/") {
		mime = http.DetectContentType(data)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), mime, nil
}

func (p *Provider) webUploadImage(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, ref, name string) (webUploadMeta, error) {
	data, mime, err := readRefImage(ctx, client, ref)
	if err != nil {
		return webUploadMeta{}, err
	}
	width, height := 1024, 1024
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	path := "/backend-api/files"
	metaBody := map[string]any{"file_name": name, "file_size": len(data), "use_case": "multimodal", "width": width, "height": height}
	payload, _ := json.Marshal(metaBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return webUploadMeta{}, err
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return webUploadMeta{}, err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return webUploadMeta{}, fmt.Errorf("gpt image2 web upload meta %d: %s", resp.StatusCode, snippet(raw, 240))
	}
	var meta struct {
		FileID    string `json:"file_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return webUploadMeta{}, err
	}
	if meta.FileID == "" || meta.UploadURL == "" {
		return webUploadMeta{}, fmt.Errorf("gpt image2 web upload missing file metadata")
	}
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, meta.UploadURL, bytes.NewReader(data))
	if err != nil {
		return webUploadMeta{}, err
	}
	putReq.Header.Set("Content-Type", mime)
	putReq.Header.Set("x-ms-blob-type", "BlockBlob")
	putReq.Header.Set("x-ms-version", "2020-04-08")
	putReq.Header.Set("Origin", base)
	putReq.Header.Set("Referer", base+"/")
	putReq.Header.Set("User-Agent", fp.UserAgent)
	resp, err = client.Do(putReq)
	if err != nil {
		return webUploadMeta{}, err
	}
	raw, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return webUploadMeta{}, fmt.Errorf("gpt image2 web upload blob %d: %s", resp.StatusCode, snippet(raw, 240))
	}
	path = "/backend-api/files/" + meta.FileID + "/uploaded"
	doneReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader("{}"))
	if err != nil {
		return webUploadMeta{}, err
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		doneReq.Header.Set(k, v)
	}
	doneReq.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(doneReq)
	if err != nil {
		return webUploadMeta{}, err
	}
	raw, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return webUploadMeta{}, fmt.Errorf("gpt image2 web upload confirm %d: %s", resp.StatusCode, snippet(raw, 240))
	}
	libraryFileID, err := p.webProcessUploadStream(ctx, client, base, fp, token, cookie, meta.FileID, name)
	if err != nil {
		return webUploadMeta{}, err
	}
	return webUploadMeta{FileID: meta.FileID, LibraryFileID: libraryFileID, FileName: name, FileSize: len(data), Mime: mime, Width: width, Height: height}, nil
}

func (p *Provider) webProcessUploadStream(ctx context.Context, client *http.Client, base string, fp webFP, token, cookie, fileID, fileName string) (string, error) {
	path := "/backend-api/files/process_upload_stream"
	body := map[string]any{
		"file_id":                  fileID,
		"use_case":                 "multimodal",
		"index_for_retrieval":      false,
		"file_name":                fileName,
		"library_persistence_mode": "opportunistic",
		"metadata":                 map[string]any{"store_in_library": true},
		"entry_surface":            "chat_composer",
	}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	for k, v := range webBaseHeaders(fp, token, path, cookie) {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gpt image2 web process upload %d: %s", resp.StatusCode, snippet(raw, 240))
	}
	libraryFileID := ""
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev struct {
			Extra struct {
				MetadataObjectID string `json:"metadata_object_id"`
			} `json:"extra"`
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Extra.MetadataObjectID != "" {
			libraryFileID = ev.Extra.MetadataObjectID
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return libraryFileID, nil
}

// === helpers ===

func strParam(p map[string]any, key, def string) string {
	if p == nil {
		return def
	}
	if v, ok := p[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

func (p *Provider) httpClient(proxyURL string) (*http.Client, error) {
	return outbound.NewClient(outbound.Options{
		ProxyURL: proxyURL,
		Timeout:  defaultTimeout,
		Mode:     outbound.ModeUTLS,
		Profile:  outbound.ProfileChrome,
	})
}

func firstStringParam(p map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := strParam(p, key, ""); v != "" {
			return v
		}
	}
	return ""
}

func copyParam(dst map[string]any, src map[string]any, key string) {
	if src == nil {
		return
	}
	if v, ok := src[key]; ok {
		switch t := v.(type) {
		case string:
			if t != "" {
				dst[key] = t
			}
		default:
			dst[key] = v
		}
	}
}

func shouldUseWebImage2(req *provider.Request) bool {
	tier := strings.ToUpper(strings.TrimSpace(strParam(req.Params, "resolution", strParam(req.Params, "size_tier", ""))))
	if tier == "" {
		size := strParam(req.Params, "size", "")
		w, h := parseSize(size)
		if size == "" || w*h <= 1500000 {
			return true
		}
		return false
	}
	return tier == "1K" || tier == "1"
}

func isGPTImage2(model string) bool {
	return imageToolModel(model) == "gpt-image-2"
}

func imageToolModel(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	return model
}

func shouldRetryImage2WithoutToolChoice(raw []byte) bool {
	msg := strings.ToLower(string(raw))
	return strings.Contains(msg, "tool choice") &&
		strings.Contains(msg, "image_generation") &&
		strings.Contains(msg, "not found") &&
		strings.Contains(msg, "tools")
}

func webImage2Diag(conversationID string, fileIDs, sedimentIDs, directURLs, urls, downloadErrs []string, text string) string {
	return fmt.Sprintf("conversation_id=%s file_ids=%d sediment_ids=%d direct_urls=%d resolved_urls=%d download_errors=%d first_download_error=%s text=%s", conversationID, len(fileIDs), len(sedimentIDs), len(directURLs), len(urls), len(downloadErrs), firstString(downloadErrs), snippet([]byte(text), 120))
}

func mainModelForImage2(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.LastIndex(model, "/"); idx > 0 {
		return model[:idx] + "/gpt-5.5"
	}
	return "gpt-5.5"
}

func responseEndpoint(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	if strings.Contains(base, "/backend-api/codex") {
		return base + "/responses"
	}
	return base + "/v1/responses"
}

func isCodexBase(base string) bool {
	return strings.Contains(strings.ToLower(base), "/backend-api/codex")
}

func isCodexEndpoint(url string) bool {
	return strings.Contains(strings.ToLower(url), "chatgpt.com/backend-api/codex")
}

func userAgentForEndpoint(url string) string {
	if isCodexEndpoint(url) {
		return "codex-tui/0.118.0 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9 (codex-tui; 0.118.0)"
	}
	return "kleinai/1.0"
}

func imageSize(params map[string]any, def string) string {
	if size := strParam(params, "size", ""); size != "" {
		return size
	}
	ratio := strParam(params, "ratio", strParam(params, "aspect_ratio", "1:1"))
	tier := strings.ToUpper(strParam(params, "resolution", strParam(params, "size_tier", "1K")))
	sizes := map[string]map[string]string{
		"1K": {
			"1:1":  "1024x1024",
			"3:2":  "1216x832",
			"2:3":  "832x1216",
			"4:3":  "1152x864",
			"3:4":  "864x1152",
			"5:4":  "1120x896",
			"4:5":  "896x1120",
			"16:9": "1344x768",
			"9:16": "768x1344",
			"21:9": "1536x640",
		},
		"2K": {
			"1:1":  "1248x1248",
			"3:2":  "1536x1024",
			"2:3":  "1024x1536",
			"4:3":  "1440x1088",
			"3:4":  "1088x1440",
			"5:4":  "1392x1120",
			"4:5":  "1120x1392",
			"16:9": "1664x928",
			"9:16": "928x1664",
			"21:9": "1904x816",
		},
		"4K": {
			"1:1":  "2480x2480",
			"3:2":  "3056x2032",
			"2:3":  "2032x3056",
			"4:3":  "2880x2160",
			"3:4":  "2160x2880",
			"5:4":  "2784x2224",
			"4:5":  "2224x2784",
			"16:9": "3312x1872",
			"9:16": "1872x3312",
			"21:9": "3808x1632",
		},
	}
	if byRatio, ok := sizes[tier]; ok {
		if size := byRatio[ratio]; size != "" {
			return size
		}
		return byRatio["1:1"]
	}
	if byRatio := sizes["1K"]; byRatio != nil {
		if size := byRatio[ratio]; size != "" {
			return size
		}
	}
	return def
}

func imageQuality(params map[string]any) string {
	switch strings.ToLower(strParam(params, "quality", "")) {
	case "draft", "low":
		return "low"
	case "standard", "medium":
		return "medium"
	case "hd", "high":
		return "high"
	default:
		return ""
	}
}

func webImageModelSlug(req *provider.Request) string {
	if req != nil {
		if v := strings.TrimSpace(strParam(req.Params, "web_model", "")); v != "" && strings.Contains(strings.ToLower(v), "thinking") {
			return v
		}
	}
	return defaultWebImageThinkingModel
}

func webImageConversationPlan(count int) (conversationLimit int, requireCompleteSet bool) {
	if count <= 1 {
		return 1, false
	}
	return 1, true
}

func webImageTestMode(req *provider.Request) webImageTestModeState {
	if req == nil || req.Count <= 1 || !isGPTImage2(req.ModelCode) || !shouldUseWebImage2(req) {
		return webImageTestModeState{}
	}
	mode := strings.TrimSpace(strParam(req.Params, "web_test_mode", ""))
	if mode != webImageWaitAllThenDownload {
		return webImageTestModeState{}
	}
	return webImageTestModeState{
		Enabled:                true,
		Mode:                   mode,
		DownloadDeferred:       true,
		StrictFailOnIncomplete: true,
	}
}

func webImageTestModeMeta(mode webImageTestModeState) map[string]any {
	if !mode.Enabled {
		return nil
	}
	return map[string]any{
		"web_test_mode":               mode.Mode,
		"download_deferred":           mode.DownloadDeferred,
		"collection_candidate_count":  mode.CollectionCandidateCount,
		"authoritative_complete":      mode.AuthoritativeComplete,
		"authoritative_stable_rounds": mode.AuthoritativeStableRounds,
		"final_download_started":      mode.FinalDownloadStarted,
		"final_download_seq_count":    mode.FinalDownloadSeqCount,
		"strict_fail_on_incomplete":   mode.StrictFailOnIncomplete,
	}
}

func mergeMeta(dst map[string]any, extras ...map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for _, extra := range extras {
		for k, v := range extra {
			dst[k] = v
		}
	}
	return dst
}

func webImagePollStepContext(parent context.Context) context.Context {
	ctx, _ := context.WithTimeout(parent, webImagePollStepTimeout)
	return ctx
}

func webLogPollStep(ctx context.Context, req *provider.Request, mode webImageTestModeState, step webImagePollStep, conversationID string, extras map[string]any, state webConversationImageState, err error) {
	if !mode.Enabled || step.Name == "" {
		return
	}
	meta := mergeMeta(map[string]any{
		"conversation_id":           conversationID,
		"step_kind":                 step.Kind,
		"file_ids":                  len(state.FileIDs),
		"sediment_ids":              len(state.SedimentIDs),
		"direct_urls":               len(state.DirectURLs),
		"authoritative_order_found": state.HasAuthoritativeOrder,
		"authoritative_count":       len(state.OrderedRefs),
	}, webImageTestModeMeta(mode), extras)
	entry := provider.UpstreamLogEntry{
		Provider: "gpt",
		Stage:    step.Name,
		Meta:     meta,
	}
	if err != nil {
		entry.Error = err.Error()
		entry.Stage = step.Name + ".failed"
	} else {
		entry.Stage = step.Name + ".ok"
	}
	logUpstream(ctx, req, entry)
}

func webBaseHeaders(fp webFP, token, path, cookie string) map[string]string {
	h := map[string]string{
		"User-Agent":                 fp.UserAgent,
		"Origin":                     "https://chatgpt.com",
		"Referer":                    "https://chatgpt.com/",
		"Accept-Language":            "zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7",
		"Cache-Control":              "no-cache",
		"Pragma":                     "no-cache",
		"Priority":                   "u=1, i",
		"Sec-Ch-Ua":                  fp.SecCHUA,
		"Sec-Ch-Ua-Arch":             `"x86"`,
		"Sec-Ch-Ua-Bitness":          `"64"`,
		"Sec-Ch-Ua-Mobile":           "?0",
		"Sec-Ch-Ua-Model":            `""`,
		"Sec-Ch-Ua-Platform":         `"` + fp.Platform + `"`,
		"Sec-Ch-Ua-Platform-Version": `"19.0.0"`,
		"Sec-Fetch-Dest":             "empty",
		"Sec-Fetch-Mode":             "cors",
		"Sec-Fetch-Site":             "same-origin",
		"OAI-Device-Id":              fp.DeviceID,
		"OAI-Session-Id":             fp.SessionID,
		"OAI-Language":               "zh-CN",
		"OAI-Client-Version":         fp.ClientVersion,
		"OAI-Client-Build-Number":    fp.BuildNumber,
		"X-OpenAI-Target-Path":       path,
		"X-OpenAI-Target-Route":      path,
	}
	if token != "" {
		h["Authorization"] = "Bearer " + token
	}
	if strings.TrimSpace(cookie) != "" {
		h["Cookie"] = strings.TrimSpace(cookie)
	}
	return h
}

func webBootstrapHeaders(fp webFP, cookie string) map[string]string {
	h := webBaseHeaders(fp, "", "/", cookie)
	h["Accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"
	h["Sec-Fetch-Dest"] = "document"
	h["Sec-Fetch-Mode"] = "navigate"
	h["Sec-Fetch-Site"] = "none"
	h["Sec-Fetch-User"] = "?1"
	h["Upgrade-Insecure-Requests"] = "1"
	delete(h, "Authorization")
	delete(h, "X-OpenAI-Target-Path")
	delete(h, "X-OpenAI-Target-Route")
	return h
}

func webImageHeaders(fp webFP, token, path, cookie string, reqs webRequirement, conduit, accept string) map[string]string {
	h := webBaseHeaders(fp, token, path, cookie)
	h["Content-Type"] = "application/json"
	h["Accept"] = accept
	h["OpenAI-Sentinel-Chat-Requirements-Token"] = reqs.Token
	if reqs.ProofToken != "" {
		h["OpenAI-Sentinel-Proof-Token"] = reqs.ProofToken
	}
	if reqs.SOToken != "" {
		h["OpenAI-Sentinel-SO-Token"] = reqs.SOToken
	}
	if conduit != "" {
		h["X-Conduit-Token"] = conduit
	}
	if accept == "text/event-stream" {
		h["X-Oai-Turn-Trace-Id"] = uuid.NewString()
	}
	return h
}

func webImagePrompt(prompt, ratio string) string {
	prompt = strings.TrimSpace(prompt)
	ratio = strings.TrimSpace(ratio)
	if ratio == "" || ratio == "1:1" {
		return prompt
	}
	hints := map[string]string{
		"16:9": "输出一张 16:9 横屏构图的图片。",
		"9:16": "输出一张 9:16 竖屏构图的图片。",
		"4:3":  "输出一张 4:3 比例的图片。",
		"3:4":  "输出一张 3:4 竖向比例的图片。",
	}
	if h, ok := hints[ratio]; ok {
		return prompt + "\n\n" + h
	}
	return prompt + "\n\n输出图片，宽高比为 " + ratio + "。"
}

func webImagePromptV2(prompt, ratio, size string, count int) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "生成一张高质量图片"
	}
	ratio = webRatioFromSize(size, ratio)
	directives := make([]string, 0, 2)
	if count > 1 {
		directives = append(directives, fmt.Sprintf("这是一次单对话连续套图任务。请在同一个对话回复中一次性生成 %d 张彼此不同、风格统一、顺序明确的图片，并按最终展示顺序输出。不要拆成多个批次，不要只返回 1 张，也不要重复同一张图片充数。", count))
	}
	ratio = strings.TrimSpace(ratio)
	if ratio != "" && ratio != "1:1" {
		directives = append(directives, "将宽高比设为 "+ratio)
	}
	if len(directives) == 0 {
		return prompt
	}
	return prompt + "\n\n" + strings.Join(directives, "\n")
}

func webRatioFromSize(size, fallback string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return strings.TrimSpace(fallback)
	}
	switch size {
	case "1024x1024", "1248x1248", "2480x2480":
		return "1:1"
	case "1216x832", "1536x1024", "3056x2032":
		return "3:2"
	case "832x1216", "1024x1536", "2032x3056":
		return "2:3"
	case "1152x864", "1440x1088", "2880x2160":
		return "4:3"
	case "864x1152", "1088x1440", "2160x2880":
		return "3:4"
	case "1120x896", "1392x1120", "2784x2224":
		return "5:4"
	case "896x1120", "1120x1392", "2224x2784":
		return "4:5"
	case "1344x768", "1664x928", "3312x1872":
		return "16:9"
	case "768x1344", "928x1664", "1872x3312":
		return "9:16"
	case "1536x640", "1904x816", "3808x1632":
		return "21:9"
	default:
		return strings.TrimSpace(fallback)
	}
}

func readRefImage(ctx context.Context, client *http.Client, ref string) ([]byte, string, error) {
	if ref == "" {
		return nil, "", fmt.Errorf("empty reference image")
	}
	if strings.HasPrefix(ref, "data:") {
		header, data, ok := strings.Cut(ref, ",")
		if !ok {
			return nil, "", fmt.Errorf("invalid data url image")
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, "", err
		}
		mime := strings.TrimPrefix(strings.Split(strings.TrimPrefix(header, "data:"), ";")[0], "data:")
		if mime == "" {
			mime = http.DetectContentType(raw)
		}
		return raw, mime, nil
	}
	if strings.HasPrefix(ref, "/api/v1/gen/cached/") {
		rel := strings.TrimPrefix(ref, "/api/v1/gen/cached/")
		if rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
			return nil, "", fmt.Errorf("invalid cached reference image")
		}
		root := strings.TrimSpace(os.Getenv("KLEIN_STORAGE_ROOT"))
		if root == "" {
			root = "/app/storage/public"
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, "", fmt.Errorf("read cached reference image: %w", err)
		}
		if len(raw) == 0 {
			return nil, "", fmt.Errorf("empty cached reference image")
		}
		return raw, http.DetectContentType(raw), nil
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, "", fmt.Errorf("reference image must be data/http url")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("reference image download %d: %s", resp.StatusCode, snippet(data, 160))
	}
	mime := resp.Header.Get("Content-Type")
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = mime[:idx]
	}
	if mime == "" || !strings.HasPrefix(mime, "image/") {
		mime = http.DetectContentType(data)
	}
	return data, mime, nil
}

func parseWebImageSSE(r io.Reader) (string, []string, []string, []string, string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var dataLines []string
	conversationID := ""
	lastText := ""
	var fileIDs, sedimentIDs, directURLs []string
	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return
		}
		cid, _, _, rawURLs := extractWebImageIDs(data)
		if cid != "" && conversationID == "" {
			conversationID = cid
		}
		addUniqueWebAssetURLs(&directURLs, rawURLs...)
		if toolFileIDs, toolSedimentIDs := extractWebImageToolIDs([]byte(data)); len(toolFileIDs) > 0 || len(toolSedimentIDs) > 0 {
			addUniqueString(&fileIDs, toolFileIDs...)
			addUniqueString(&sedimentIDs, toolSedimentIDs...)
		}
		if text := extractWebAssistantText(data); text != "" {
			lastText = text
		}

		var ev responseCompletedEvent
		_ = json.Unmarshal([]byte(data), &ev)
		var direct struct {
			Output []responseOutputItem `json:"output"`
			Item   responseOutputItem   `json:"item"`
		}
		if err := json.Unmarshal([]byte(data), &direct); err == nil {
			if len(ev.Response.Output) == 0 && len(direct.Output) > 0 {
				ev.Type = "response.completed"
				ev.Response.Output = direct.Output
			}
			if direct.Item.Type != "" && ev.Type == "" {
				ev.Type = "response.output_item.done"
			}
		}
		switch ev.Type {
		case "response.output_item.done":
			if direct.Item.Type != "" {
				if dataURL, imageURL := outputImagePayload(direct.Item); dataURL != "" || imageURL != "" {
					if imageURL != "" {
						addUniqueWebAssetURLs(&directURLs, imageURL)
					} else {
						mime := mimeForImageFormat(direct.Item.OutputFormat)
						if mime == "" {
							mime = "image/png"
						}
						addUniqueString(&directURLs, "data:"+mime+";base64,"+dataURL)
					}
				}
			}
		case "response.completed":
			for _, out := range ev.Response.Output {
				if dataURL, imageURL := outputImagePayload(out); dataURL != "" || imageURL != "" {
					if imageURL != "" {
						addUniqueWebAssetURLs(&directURLs, imageURL)
						continue
					}
					mime := mimeForImageFormat(out.OutputFormat)
					if mime == "" {
						mime = "image/png"
					}
					addUniqueString(&directURLs, "data:"+mime+";base64,"+dataURL)
				}
			}
		case "response.image_generation_call.partial_image":
			var partial struct {
				OutputFormat string `json:"output_format"`
				PartialB64   string `json:"partial_image_b64"`
			}
			if err := json.Unmarshal([]byte(data), &partial); err == nil && partial.PartialB64 != "" {
				mime := mimeForImageFormat(partial.OutputFormat)
				if mime == "" {
					mime = "image/png"
				}
				addUniqueString(&directURLs, "data:"+mime+";base64,"+partial.PartialB64)
			}
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return "", nil, nil, nil, "", fmt.Errorf("gpt image2 web stream read: %w", err)
	}
	return conversationID, fileIDs, sedimentIDs, directURLs, lastText, nil
}

var (
	webConversationIDRe   = regexp.MustCompile(`"conversation_id"\s*:\s*"([^"]+)"`)
	webFileIDRe           = regexp.MustCompile(`file[-_][A-Za-z0-9][A-Za-z0-9_-]{7,}`)
	webSedimentIDRe       = regexp.MustCompile(`sediment://([A-Za-z0-9_-]+)`)
	webAssetURLRe         = regexp.MustCompile(`https://(?:files\.oaiusercontent\.com|oaidalleapiprodscus\.blob\.core\.windows\.net)[^"\s]+`)
	webRelativeAssetURLRe = regexp.MustCompile(`/backend-api/(?:files/download/[^"\s]+|conversation/[^"\s]+/attachment/[^"\s]+/download[^"\s]*|estuary/content\?[^"\s]*id=file_[^"\s]+)`)
)

func extractWebImageIDs(payload string) (string, []string, []string, []string) {
	conversationID := ""
	if m := webConversationIDRe.FindStringSubmatch(payload); len(m) > 1 {
		conversationID = m[1]
	}
	var fileIDs, sedimentIDs, directURLs []string
	normalizedPayload := strings.ReplaceAll(payload, `\/`, `/`)
	normalizedPayload = strings.ReplaceAll(normalizedPayload, `\u0026`, `&`)
	for _, id := range webFileIDRe.FindAllString(payload, -1) {
		addUniqueString(&fileIDs, id)
	}
	for _, m := range webSedimentIDRe.FindAllStringSubmatch(payload, -1) {
		if len(m) > 1 {
			addUniqueString(&sedimentIDs, m[1])
		}
	}
	for _, raw := range webAssetURLRe.FindAllString(normalizedPayload, -1) {
		u := strings.TrimSpace(raw)
		if strings.Contains(u, "openaiassets.blob.core.windows.net/$web/chatgpt/") {
			continue
		}
		addUniqueWebAssetURLs(&directURLs, u)
	}
	for _, raw := range webRelativeAssetURLRe.FindAllString(normalizedPayload, -1) {
		addUniqueWebAssetURLs(&directURLs, strings.TrimSpace(raw))
	}
	return conversationID, fileIDs, sedimentIDs, directURLs
}

func extractWebImageDirectURLs(payload string) []string {
	_, _, _, directURLs := extractWebImageIDs(payload)
	return directURLs
}

func extractWebImageToolIDs(raw []byte) ([]string, []string) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, nil
	}
	var fileIDs, sedimentIDs []string
	walkWebImageToolMessages(v, &fileIDs, &sedimentIDs)
	return fileIDs, sedimentIDs
}

func walkWebImageToolMessages(v any, fileIDs, sedimentIDs *[]string) {
	switch t := v.(type) {
	case map[string]any:
		if msg, ok := asWebMessageMap(t); ok && isWebImageAssetMessage(msg) {
			extractWebAssetPointersFromMessage(msg, fileIDs, sedimentIDs)
		}
		for _, val := range t {
			walkWebImageToolMessages(val, fileIDs, sedimentIDs)
		}
	case []any:
		for _, val := range t {
			walkWebImageToolMessages(val, fileIDs, sedimentIDs)
		}
	}
}

func extractWebAuthoritativeOrderedRefs(raw []byte, refs []webUploadMeta) ([]webOrderedRef, bool) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	out := make([]webOrderedRef, 0)
	exclude := webReferenceAssetExclude(refs)
	walkWebAuthoritativeImageMessages(v, exclude, &out)
	out = filterUniqueWebOrderedRefs(out)
	return out, len(out) > 0
}

func walkWebAuthoritativeImageMessages(v any, exclude map[string]bool, out *[]webOrderedRef) {
	switch t := v.(type) {
	case map[string]any:
		if msg, ok := asWebMessageMap(t); ok && isWebImageAssetMessage(msg) {
			collectWebAuthoritativeRefsFromMessage(msg, exclude, out)
		}
		if msgs, ok := t["messages"].([]any); ok {
			for _, item := range msgs {
				walkWebAuthoritativeImageMessages(item, exclude, out)
			}
			return
		}
		for _, val := range t {
			walkWebAuthoritativeImageMessages(val, exclude, out)
		}
	case []any:
		for _, val := range t {
			walkWebAuthoritativeImageMessages(val, exclude, out)
		}
	}
}

func collectWebAuthoritativeRefsFromMessage(msg map[string]any, exclude map[string]bool, out *[]webOrderedRef) {
	metadata, _ := msg["metadata"].(map[string]any)
	content, _ := msg["content"].(map[string]any)
	appendWebOrderedRefsFromArray(metadata["attachments"], "metadata.attachments", exclude, out)
	appendWebOrderedRefsFromArray(metadata["citations"], "metadata.citations", exclude, out)
	appendWebOrderedRefsFromArray(metadata["conversation_context_citation_metadata"], "metadata.conversation_context_citation_metadata", exclude, out)
	appendWebOrderedRefsFromArray(content["parts"], "content.parts", exclude, out)
}

func appendWebOrderedRefsFromArray(v any, source string, exclude map[string]bool, out *[]webOrderedRef) {
	items, ok := v.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		if ref, ok := extractWebOrderedRef(item, source, exclude); ok {
			*out = append(*out, ref)
		}
	}
}

func extractWebOrderedRef(v any, source string, exclude map[string]bool) (webOrderedRef, bool) {
	switch t := v.(type) {
	case map[string]any:
		if ptr := strings.TrimSpace(fmt.Sprint(t["asset_pointer"])); ptr != "" {
			ref := orderedRefFromAssetPointer(ptr, source)
			if !isExcludedWebOrderedRef(ref, exclude) && webOrderedRefKey(ref) != "" {
				return ref, true
			}
		}
		if ref := orderedRefFromMap(t, source); webOrderedRefKey(ref) != "" && !isExcludedWebOrderedRef(ref, exclude) {
			return ref, true
		}
		for _, val := range t {
			if ref, ok := extractWebOrderedRef(val, source, exclude); ok {
				return ref, true
			}
		}
	case []any:
		for _, val := range t {
			if ref, ok := extractWebOrderedRef(val, source, exclude); ok {
				return ref, true
			}
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return webOrderedRef{}, false
		}
		if strings.HasPrefix(s, "file-service://") || strings.HasPrefix(s, "sediment://") {
			ref := orderedRefFromAssetPointer(s, source)
			if webOrderedRefKey(ref) != "" && !isExcludedWebOrderedRef(ref, exclude) {
				return ref, true
			}
		}
		if isGeneratedWebAssetURL(s) {
			ref := webOrderedRef{RawURL: s, Source: source}
			if webOrderedRefKey(ref) != "" && !isExcludedWebOrderedRef(ref, exclude) {
				return ref, true
			}
		}
	}
	return webOrderedRef{}, false
}

func orderedRefFromMap(m map[string]any, source string) webOrderedRef {
	ref := webOrderedRef{Source: source}
	for _, key := range []string{"id", "file_id", "ref_id"} {
		if fileID := strings.TrimSpace(fmt.Sprint(m[key])); strings.HasPrefix(fileID, "file_") {
			ref.FileID = fileID
			break
		}
	}
	for _, key := range []string{"download_url", "attachment_url", "estuary_url", "url"} {
		if u := strings.TrimSpace(fmt.Sprint(m[key])); isGeneratedWebAssetURL(u) {
			ref.RawURL = u
			if ref.FileID == "" {
				ref.FileID = extractWebFileIDFromURL(u)
			}
			if ref.SedimentID == "" {
				ref.SedimentID = extractWebSedimentIDFromURL(u)
			}
			break
		}
	}
	if nested, ok := m["attachment"].(map[string]any); ok {
		if ref.FileID == "" {
			if fileID := strings.TrimSpace(fmt.Sprint(nested["file_id"])); strings.HasPrefix(fileID, "file_") {
				ref.FileID = fileID
			}
		}
	}
	return ref
}

func orderedRefFromAssetPointer(ptr, source string) webOrderedRef {
	ref := webOrderedRef{Source: source}
	switch {
	case strings.HasPrefix(ptr, "file-service://"):
		ref.FileID = strings.TrimPrefix(ptr, "file-service://")
	case strings.HasPrefix(ptr, "sediment://"):
		ref.SedimentID = strings.TrimPrefix(ptr, "sediment://")
		if strings.HasPrefix(ref.SedimentID, "file_") {
			ref.FileID = ref.SedimentID
		}
	}
	return ref
}

func webReferenceAssetExclude(refs []webUploadMeta) map[string]bool {
	exclude := map[string]bool{}
	for _, ref := range refs {
		if ref.FileID != "" {
			exclude["file:"+ref.FileID] = true
		}
		if ref.LibraryFileID != "" {
			exclude["file:"+ref.LibraryFileID] = true
		}
	}
	return exclude
}

func isExcludedWebOrderedRef(ref webOrderedRef, exclude map[string]bool) bool {
	if exclude == nil {
		return false
	}
	if ref.FileID != "" && exclude["file:"+ref.FileID] {
		return true
	}
	return false
}

func filterUniqueWebOrderedRefs(in []webOrderedRef) []webOrderedRef {
	out := make([]webOrderedRef, 0, len(in))
	seen := map[string]bool{}
	for _, ref := range in {
		key := webOrderedRefKey(ref)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ref)
	}
	return out
}

func asWebMessageMap(m map[string]any) (map[string]any, bool) {
	if msg, ok := m["message"].(map[string]any); ok {
		return msg, true
	}
	if _, ok := m["author"].(map[string]any); ok {
		return m, true
	}
	return nil, false
}

func isWebImageAssetMessage(msg map[string]any) bool {
	author, _ := msg["author"].(map[string]any)
	metadata, _ := msg["metadata"].(map[string]any)
	content, _ := msg["content"].(map[string]any)
	role := strings.ToLower(strings.TrimSpace(fmt.Sprint(author["role"])))
	taskType := strings.ToLower(strings.TrimSpace(fmt.Sprint(metadata["async_task_type"])))
	contentType := strings.ToLower(strings.TrimSpace(fmt.Sprint(content["content_type"])))
	if role != "tool" && role != "assistant" {
		return false
	}
	if taskType == "" {
		taskType = strings.ToLower(strings.TrimSpace(fmt.Sprint(metadata["task_type"])))
	}
	if taskType != "" && !strings.Contains(taskType, "image") && !strings.Contains(taskType, "picture") {
		return false
	}
	return strings.Contains(contentType, "text") || strings.Contains(contentType, "image")
}

func extractWebAssetPointersFromMessage(msg map[string]any, fileIDs, sedimentIDs *[]string) {
	content, _ := msg["content"].(map[string]any)
	walkWebAssetPointers(content, fileIDs, sedimentIDs)
	metadata, _ := msg["metadata"].(map[string]any)
	extractWebMetadataAssetIDs(metadata, fileIDs, sedimentIDs)
}

func extractWebMetadataAssetIDs(metadata map[string]any, fileIDs, sedimentIDs *[]string) {
	if metadata == nil {
		return
	}
	if attachments, ok := metadata["attachments"].([]any); ok {
		for _, item := range attachments {
			if m, ok := item.(map[string]any); ok {
				addWebFileID(fileIDs, fmt.Sprint(m["id"]))
				addWebFileID(fileIDs, fmt.Sprint(m["file_id"]))
			}
		}
	}
	if citations, ok := metadata["citations"].([]any); ok {
		for _, item := range citations {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			addWebFileID(fileIDs, fmt.Sprint(m["file_id"]))
			if nested, ok := m["metadata"].(map[string]any); ok {
				addWebFileID(fileIDs, fmt.Sprint(nested["file_id"]))
			}
		}
	}
	appendWebMetadataAssetIDs(metadata["content_references_by_file"], fileIDs, sedimentIDs)
	appendWebMetadataAssetIDs(metadata["conversation_context_citation_metadata"], fileIDs, sedimentIDs)
	appendWebMetadataAssetIDs(metadata["image_results"], fileIDs, sedimentIDs)
	appendWebMetadataAssetIDs(metadata["result"], fileIDs, sedimentIDs)
}

func appendWebMetadataAssetIDs(v any, fileIDs, sedimentIDs *[]string) {
	walkWebMetadataAssetIDs("", v, fileIDs, sedimentIDs)
}

func walkWebMetadataAssetIDs(key string, v any, fileIDs, sedimentIDs *[]string) {
	switch t := v.(type) {
	case map[string]any:
		if ptr := strings.TrimSpace(fmt.Sprint(t["asset_pointer"])); ptr != "" {
			addWebAssetPointer(ptr, fileIDs, sedimentIDs)
		}
		for nestedKey, val := range t {
			walkWebMetadataAssetIDs(nestedKey, val, fileIDs, sedimentIDs)
		}
	case []any:
		for _, val := range t {
			walkWebMetadataAssetIDs(key, val, fileIDs, sedimentIDs)
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return
		}
		if strings.HasPrefix(s, "file-service://") || strings.HasPrefix(s, "sediment://") {
			addWebAssetPointer(s, fileIDs, sedimentIDs)
			return
		}
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if strings.HasPrefix(s, "file_") && (lowerKey == "id" || lowerKey == "file_id" || lowerKey == "ref_id" || strings.Contains(lowerKey, "file") || strings.Contains(lowerKey, "asset") || strings.Contains(lowerKey, "attachment") || strings.Contains(lowerKey, "reference")) {
			addWebFileID(fileIDs, s)
		}
	}
}

func walkWebAssetPointers(v any, fileIDs, sedimentIDs *[]string) {
	switch t := v.(type) {
	case map[string]any:
		if ptr := strings.TrimSpace(fmt.Sprint(t["asset_pointer"])); ptr != "" {
			addWebAssetPointer(ptr, fileIDs, sedimentIDs)
		}
		for _, val := range t {
			walkWebAssetPointers(val, fileIDs, sedimentIDs)
		}
	case []any:
		for _, val := range t {
			walkWebAssetPointers(val, fileIDs, sedimentIDs)
		}
	case string:
		addWebAssetPointer(t, fileIDs, sedimentIDs)
	}
}

func addWebAssetPointer(ptr string, fileIDs, sedimentIDs *[]string) {
	switch {
	case strings.HasPrefix(ptr, "file-service://"):
		addWebFileID(fileIDs, strings.TrimPrefix(ptr, "file-service://"))
	case strings.HasPrefix(ptr, "sediment://"):
		id := strings.TrimPrefix(ptr, "sediment://")
		if id != "" {
			addUniqueString(sedimentIDs, id)
		}
	}
}

func addWebFileID(fileIDs *[]string, raw string) {
	id := strings.TrimSpace(raw)
	if id == "" || id == "file_upload" || !strings.HasPrefix(id, "file_") {
		return
	}
	addUniqueString(fileIDs, id)
}

func extractWebAssistantText(payload string) string {
	var ev any
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return ""
	}
	return findFirstStringByKey(ev, "parts")
}

func findFirstStringByKey(v any, key string) string {
	switch t := v.(type) {
	case map[string]any:
		if val, ok := t[key]; ok {
			if arr, ok := val.([]any); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s)
					}
				}
			}
		}
		for _, val := range t {
			if s := findFirstStringByKey(val, key); s != "" {
				return s
			}
		}
	case []any:
		for _, val := range t {
			if s := findFirstStringByKey(val, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func addUniqueString(dst *[]string, vals ...string) {
	for _, v := range vals {
		if v == "" {
			continue
		}
		exists := false
		for _, cur := range *dst {
			if cur == v {
				exists = true
				break
			}
		}
		if !exists {
			*dst = append(*dst, v)
		}
	}
}

func mergeOrderedUniqueStrings(groups ...[]string) []string {
	out := make([]string, 0)
	for _, group := range groups {
		addUniqueString(&out, group...)
	}
	return out
}

func addUniqueWebAssetURLs(dst *[]string, vals ...string) {
	for _, v := range vals {
		if isGeneratedWebAssetURL(v) {
			addUniqueString(dst, v)
		}
	}
}

func mergeOrderedWebAssetURLs(groups ...[]string) []string {
	out := make([]string, 0)
	for _, group := range groups {
		addUniqueWebAssetURLs(&out, group...)
	}
	return out
}

func newWebImageCandidatePool() *webImageCandidatePool {
	return &webImageCandidatePool{
		aliases:    map[string]*webImageCandidate{},
		candidates: make([]*webImageCandidate, 0, 8),
	}
}

func ensureWebImageCandidate(pool *webImageCandidatePool, rawURL string) *webImageCandidate {
	if pool == nil {
		return nil
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	if alias := webURLAlias(rawURL); alias != "" {
		if c := pool.aliases[alias]; c != nil {
			addWebImageCandidateAlias(pool, c, alias)
			addWebImageCandidateRawURL(c, rawURL)
			backfillWebImageCandidateIDsFromURL(pool, c, rawURL)
			return canonicalWebImageCandidate(c)
		}
	}
	c := &webImageCandidate{
		rawURLs:                      []string{},
		authoritativeFinalOrderIndex: -1,
		directOrderIndex:             -1,
		fileIDOrderIndex:             -1,
		sedimentIDOrderIndex:         -1,
		firstSeenPollCount:           -1,
	}
	pool.candidates = append(pool.candidates, c)
	addWebImageCandidateRawURL(c, rawURL)
	addWebImageCandidateAlias(pool, c, webURLAlias(rawURL))
	backfillWebImageCandidateIDsFromURL(pool, c, rawURL)
	return c
}

func updateWebImageCandidateOrder(candidate *webImageCandidate, state webConversationImageState, resolvedIndex, pollCount int) {
	candidate = canonicalWebImageCandidate(candidate)
	if candidate == nil {
		return
	}
	if candidate.firstSeenPollCount < 0 {
		candidate.firstSeenPollCount = pollCount
	}
	if candidate.directOrderIndex < 0 {
		for idx, u := range state.DirectURLs {
			if webCandidateMatchesURL(candidate, u) {
				candidate.directOrderIndex = idx
				break
			}
		}
	}
	if candidate.fileIDOrderIndex < 0 && candidate.fileID != "" {
		for idx, id := range state.FileIDs {
			if id == candidate.fileID {
				candidate.fileIDOrderIndex = idx
				break
			}
		}
	}
	if candidate.sedimentIDOrderIndex < 0 && candidate.sedimentID != "" {
		for idx, id := range state.SedimentIDs {
			if id == candidate.sedimentID {
				candidate.sedimentIDOrderIndex = idx
				break
			}
		}
	}
	if candidate.fileIDOrderIndex < 0 && candidate.fileID == "" && resolvedIndex >= 0 {
		candidate.fileIDOrderIndex = resolvedIndex
	}
	if candidate.sedimentIDOrderIndex < 0 && candidate.sedimentID == "" && resolvedIndex >= 0 {
		candidate.sedimentIDOrderIndex = resolvedIndex
	}
}

func countWebImageCandidatesWithData(pool *webImageCandidatePool) int {
	count := 0
	if pool == nil {
		return 0
	}
	seen := map[*webImageCandidate]bool{}
	for _, c := range pool.candidates {
		c = canonicalWebImageCandidate(c)
		if c != nil && c.dataURL != "" && !seen[c] {
			seen[c] = true
			count++
		}
	}
	return count
}

func buildOrderedWebAssets(pool *webImageCandidatePool, limit, width, height int, ratio string) []provider.Asset {
	candidates := make([]*webImageCandidate, 0, len(pool.candidates))
	seen := map[*webImageCandidate]bool{}
	for _, c := range pool.candidates {
		c = canonicalWebImageCandidate(c)
		if c == nil || c.dataURL == "" || seen[c] {
			continue
		}
		seen[c] = true
		candidates = append(candidates, c)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareWebImageCandidate(candidates[i], candidates[j])
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	assets := make([]provider.Asset, 0, len(candidates))
	for _, c := range candidates {
		mime := c.mime
		if mime == "" {
			mime = "image/png"
		}
		assets = append(assets, provider.Asset{
			URL:    c.dataURL,
			Width:  width,
			Height: height,
			Mime:   mime,
			Meta: map[string]any{
				"provider_route":                  "chatgpt_web",
				"provider_order_source":           webProviderOrderSource(c),
				"size":                            "1K",
				"ratio":                           ratio,
				"authoritative_final_order_index": c.authoritativeFinalOrderIndex,
				"authoritative_snapshot_complete": c.authoritativeFinalOrderIndex >= 0,
				"direct_order_index":              c.directOrderIndex,
				"file_id_order_index":             c.fileIDOrderIndex,
				"sediment_order_index":            c.sedimentIDOrderIndex,
				"first_seen_poll_count":           c.firstSeenPollCount,
			},
		})
	}
	return assets
}

func mergeWebConversationImageState(base, extra webConversationImageState) webConversationImageState {
	if extra.HasAuthoritativeOrder && len(extra.OrderedRefs) > 0 {
		base.OrderedRefs = append([]webOrderedRef(nil), extra.OrderedRefs...)
	}
	base.FileIDs = mergeOrderedUniqueStrings(base.FileIDs, extra.FileIDs)
	base.SedimentIDs = mergeOrderedUniqueStrings(base.SedimentIDs, extra.SedimentIDs)
	base.DirectURLs = mergeOrderedWebAssetURLs(base.DirectURLs, extra.DirectURLs)
	base.HasAuthoritativeOrder = base.HasAuthoritativeOrder || extra.HasAuthoritativeOrder
	return base
}

func mergeOrderedWebOrderedRefs(groups ...[]webOrderedRef) []webOrderedRef {
	out := make([]webOrderedRef, 0)
	seen := map[string]bool{}
	for _, group := range groups {
		for _, ref := range group {
			key := webOrderedRefKey(ref)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ref)
		}
	}
	return out
}

func webOrderedRefKey(ref webOrderedRef) string {
	switch {
	case ref.FileID != "":
		return "file:" + ref.FileID
	case ref.SedimentID != "":
		return "sed:" + ref.SedimentID
	case strings.TrimSpace(ref.RawURL) != "":
		return webURLAlias(ref.RawURL)
	default:
		return ""
	}
}

func webApplyAuthoritativeOrder(pool *webImageCandidatePool, refs []webOrderedRef) {
	if pool == nil {
		return
	}
	for _, c := range pool.candidates {
		c = canonicalWebImageCandidate(c)
		if c != nil {
			c.authoritativeFinalOrderIndex = -1
			c.authoritativeSource = ""
		}
	}
	for idx, ref := range refs {
		c := ensureWebImageCandidateForOrderedRef(pool, ref)
		if c == nil || c.authoritativeFinalOrderIndex >= 0 {
			continue
		}
		c.authoritativeFinalOrderIndex = idx
		c.authoritativeSource = ref.Source
	}
}

func countWebImageCandidates(pool *webImageCandidatePool) int {
	if pool == nil {
		return 0
	}
	count := 0
	seen := map[*webImageCandidate]bool{}
	for _, c := range pool.candidates {
		c = canonicalWebImageCandidate(c)
		if c == nil || seen[c] {
			continue
		}
		seen[c] = true
		count++
	}
	return count
}

func webAuthoritativeOrderComplete(state webConversationImageState, count int) bool {
	return count > 0 && state.HasAuthoritativeOrder && len(state.OrderedRefs) >= count
}

func webAuthoritativeStableRounds(state webConversationImageState, count int, snapshot string, stableRounds int) (string, int) {
	nextSnapshot := authoritativeSnapshotKey(state.OrderedRefs, count)
	if nextSnapshot == "" {
		return "", 0
	}
	if nextSnapshot == snapshot {
		return snapshot, stableRounds + 1
	}
	return nextSnapshot, 1
}

func webUpdateCandidatePoolFromResolvedURLs(pool *webImageCandidatePool, state webConversationImageState, urls []string, pollCount int) {
	for idx, u := range urls {
		candidate := ensureWebImageCandidate(pool, u)
		updateWebImageCandidateOrder(candidate, state, idx, pollCount)
	}
}

func buildFinalOrderedWebCandidates(pool *webImageCandidatePool, limit int) []*webImageCandidate {
	if pool == nil {
		return nil
	}
	candidates := make([]*webImageCandidate, 0, len(pool.candidates))
	seen := map[*webImageCandidate]bool{}
	for _, c := range pool.candidates {
		c = canonicalWebImageCandidate(c)
		if c == nil || seen[c] {
			continue
		}
		seen[c] = true
		candidates = append(candidates, c)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareWebImageCandidate(candidates[i], candidates[j])
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func firstWebImageDownloadURL(candidate *webImageCandidate) string {
	candidate = canonicalWebImageCandidate(candidate)
	if candidate == nil {
		return ""
	}
	if candidate.fileID != "" {
		return "/backend-api/files/download/" + candidate.fileID
	}
	if candidate.sedimentID != "" {
		return "/backend-api/files/" + candidate.sedimentID + "/download"
	}
	for _, raw := range candidate.rawURLs {
		if isGeneratedWebAssetURL(raw) {
			return raw
		}
	}
	return ""
}

func ensureWebImageCandidateForOrderedRef(pool *webImageCandidatePool, ref webOrderedRef) *webImageCandidate {
	if pool == nil {
		return nil
	}
	var c *webImageCandidate
	if ref.FileID != "" {
		c = pool.aliases["file:"+ref.FileID]
	}
	if c == nil && ref.SedimentID != "" {
		c = pool.aliases["sed:"+ref.SedimentID]
	}
	if c == nil && ref.RawURL != "" {
		c = ensureWebImageCandidate(pool, ref.RawURL)
	}
	if c == nil {
		c = &webImageCandidate{
			rawURLs:                      []string{},
			authoritativeFinalOrderIndex: -1,
			directOrderIndex:             -1,
			fileIDOrderIndex:             -1,
			sedimentIDOrderIndex:         -1,
			firstSeenPollCount:           -1,
		}
		pool.candidates = append(pool.candidates, c)
	}
	c = canonicalWebImageCandidate(c)
	if ref.FileID != "" {
		c.fileID = ref.FileID
		addWebImageCandidateAlias(pool, c, "file:"+ref.FileID)
	}
	if ref.SedimentID != "" {
		c.sedimentID = ref.SedimentID
		addWebImageCandidateAlias(pool, c, "sed:"+ref.SedimentID)
	}
	if ref.RawURL != "" {
		addWebImageCandidateRawURL(c, ref.RawURL)
		addWebImageCandidateAlias(pool, c, webURLAlias(ref.RawURL))
		backfillWebImageCandidateIDsFromURL(pool, c, ref.RawURL)
	}
	return c
}

func webSettleImageOrder(ctx context.Context, p *Provider, client *http.Client, base string, fp webFP, token, cookie, conversationID string, refs []webUploadMeta, current webConversationImageState, count int, deadline time.Time) (webConversationImageState, int, bool) {
	if conversationID == "" || count <= 1 {
		return current, 0, false
	}
	settleDeadline := time.Now().Add(30 * time.Second)
	if settleDeadline.After(deadline) {
		settleDeadline = deadline
	}
	snapshot := authoritativeSnapshotKey(current.OrderedRefs, count)
	stableRounds := 0
	settlePollCount := 0
	for time.Now().Before(settleDeadline) {
		select {
		case <-ctx.Done():
			return current, settlePollCount, false
		case <-time.After(5 * time.Second):
		}
		next, err := p.webConversationImageIDs(ctx, client, base, fp, token, cookie, conversationID, refs)
		settlePollCount++
		if err != nil {
			continue
		}
		current = mergeWebConversationImageState(current, next)
		nextSnapshot := authoritativeSnapshotKey(current.OrderedRefs, count)
		if len(current.OrderedRefs) >= count && nextSnapshot != "" {
			if nextSnapshot == snapshot {
				stableRounds++
			} else {
				stableRounds = 1
				snapshot = nextSnapshot
			}
			if stableRounds >= 2 {
				return current, settlePollCount, true
			}
		}
	}
	return current, settlePollCount, false
}

func authoritativeSnapshotKey(refs []webOrderedRef, limit int) string {
	if limit > 0 && len(refs) < limit {
		return ""
	}
	if limit <= 0 || limit > len(refs) {
		limit = len(refs)
	}
	keys := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		keys = append(keys, webOrderedRefKey(refs[i]))
	}
	return strings.Join(keys, "|")
}

func canonicalWebImageCandidate(c *webImageCandidate) *webImageCandidate {
	for c != nil && c.mergedInto != nil {
		c = c.mergedInto
	}
	return c
}

func addWebImageCandidateAlias(pool *webImageCandidatePool, c *webImageCandidate, alias string) {
	if pool == nil || c == nil || strings.TrimSpace(alias) == "" {
		return
	}
	c = canonicalWebImageCandidate(c)
	if existing := canonicalWebImageCandidate(pool.aliases[alias]); existing != nil && existing != c {
		mergeWebImageCandidates(pool, existing, c)
		c = canonicalWebImageCandidate(existing)
	}
	pool.aliases[alias] = c
}

func addWebImageCandidateRawURL(c *webImageCandidate, rawURL string) {
	c = canonicalWebImageCandidate(c)
	rawURL = strings.TrimSpace(rawURL)
	if c == nil || rawURL == "" {
		return
	}
	for _, existing := range c.rawURLs {
		if existing == rawURL {
			return
		}
	}
	c.rawURLs = append(c.rawURLs, rawURL)
}

func backfillWebImageCandidateIDsFromURL(pool *webImageCandidatePool, c *webImageCandidate, rawURL string) {
	c = canonicalWebImageCandidate(c)
	if c == nil {
		return
	}
	if fileID := extractWebFileIDFromURL(rawURL); fileID != "" {
		c.fileID = fileID
		addWebImageCandidateAlias(pool, c, "file:"+fileID)
	}
	if sedimentID := extractWebSedimentIDFromURL(rawURL); sedimentID != "" {
		c.sedimentID = sedimentID
		addWebImageCandidateAlias(pool, c, "sed:"+sedimentID)
	}
}

func mergeWebImageCandidateByContentHash(pool *webImageCandidatePool, c *webImageCandidate) *webImageCandidate {
	c = canonicalWebImageCandidate(c)
	if pool == nil || c == nil || c.contentHash == "" {
		return c
	}
	alias := "data:" + c.contentHash
	if existing := canonicalWebImageCandidate(pool.aliases[alias]); existing != nil && existing != c {
		mergeWebImageCandidates(pool, existing, c)
		c = canonicalWebImageCandidate(existing)
	}
	pool.aliases[alias] = c
	return c
}

func mergeWebImageCandidates(pool *webImageCandidatePool, dst, src *webImageCandidate) *webImageCandidate {
	dst = canonicalWebImageCandidate(dst)
	src = canonicalWebImageCandidate(src)
	if dst == nil {
		return src
	}
	if src == nil || src == dst {
		return dst
	}
	if dst.fileID == "" {
		dst.fileID = src.fileID
	}
	if dst.sedimentID == "" {
		dst.sedimentID = src.sedimentID
	}
	if dst.contentHash == "" {
		dst.contentHash = src.contentHash
	}
	for _, rawURL := range src.rawURLs {
		addWebImageCandidateRawURL(dst, rawURL)
		addWebImageCandidateAlias(pool, dst, webURLAlias(rawURL))
	}
	if dst.authoritativeFinalOrderIndex < 0 || (src.authoritativeFinalOrderIndex >= 0 && src.authoritativeFinalOrderIndex < dst.authoritativeFinalOrderIndex) {
		dst.authoritativeFinalOrderIndex = src.authoritativeFinalOrderIndex
		if src.authoritativeSource != "" {
			dst.authoritativeSource = src.authoritativeSource
		}
	}
	if dst.authoritativeSource == "" && src.authoritativeSource != "" {
		dst.authoritativeSource = src.authoritativeSource
	}
	if dst.directOrderIndex < 0 || (src.directOrderIndex >= 0 && src.directOrderIndex < dst.directOrderIndex) {
		dst.directOrderIndex = src.directOrderIndex
	}
	if dst.fileIDOrderIndex < 0 || (src.fileIDOrderIndex >= 0 && src.fileIDOrderIndex < dst.fileIDOrderIndex) {
		dst.fileIDOrderIndex = src.fileIDOrderIndex
	}
	if dst.sedimentIDOrderIndex < 0 || (src.sedimentIDOrderIndex >= 0 && src.sedimentIDOrderIndex < dst.sedimentIDOrderIndex) {
		dst.sedimentIDOrderIndex = src.sedimentIDOrderIndex
	}
	if dst.firstSeenPollCount < 0 || (src.firstSeenPollCount >= 0 && src.firstSeenPollCount < dst.firstSeenPollCount) {
		dst.firstSeenPollCount = src.firstSeenPollCount
	}
	if dst.downloadSuccessOrder < 0 || (src.downloadSuccessOrder > 0 && (dst.downloadSuccessOrder <= 0 || src.downloadSuccessOrder < dst.downloadSuccessOrder)) {
		dst.downloadSuccessOrder = src.downloadSuccessOrder
	}
	if dst.dataURL == "" && src.dataURL != "" {
		dst.dataURL = src.dataURL
	}
	if dst.mime == "" && src.mime != "" {
		dst.mime = src.mime
	}
	if dst.fileID != "" {
		addWebImageCandidateAlias(pool, dst, "file:"+dst.fileID)
	}
	if dst.sedimentID != "" {
		addWebImageCandidateAlias(pool, dst, "sed:"+dst.sedimentID)
	}
	if dst.contentHash != "" {
		pool.aliases["data:"+dst.contentHash] = dst
	}
	src.mergedInto = dst
	return dst
}

func webURLAlias(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "data:") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "/backend-api/") {
		return "url:" + rawURL
	}
	if parsed, err := url.Parse(rawURL); err == nil {
		if parsed.Path != "" && strings.HasPrefix(parsed.Path, "/backend-api/") {
			if parsed.RawQuery != "" {
				return "url:" + parsed.Path + "?" + parsed.RawQuery
			}
			return "url:" + parsed.Path
		}
	}
	return "url:" + rawURL
}

func webCandidateMatchesURL(c *webImageCandidate, rawURL string) bool {
	c = canonicalWebImageCandidate(c)
	if c == nil {
		return false
	}
	alias := webURLAlias(rawURL)
	for _, item := range c.rawURLs {
		if webURLAlias(item) == alias {
			return true
		}
	}
	if c.fileID != "" && extractWebFileIDFromURL(rawURL) == c.fileID {
		return true
	}
	if c.sedimentID != "" && extractWebSedimentIDFromURL(rawURL) == c.sedimentID {
		return true
	}
	return false
}

func extractWebFileIDFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if m := regexp.MustCompile(`/files/download/(file_[A-Za-z0-9_-]+)`).FindStringSubmatch(rawURL); len(m) > 1 {
		return m[1]
	}
	if strings.Contains(rawURL, "id=file_") {
		if parsed, err := url.Parse(rawURL); err == nil {
			return parsed.Query().Get("id")
		}
	}
	return ""
}

func extractWebSedimentIDFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if m := regexp.MustCompile(`/attachment/([A-Za-z0-9_-]+)/download`).FindStringSubmatch(rawURL); len(m) > 1 {
		return m[1]
	}
	return ""
}

func webImageContentHash(dataURL string) string {
	dataURL = strings.TrimSpace(dataURL)
	if dataURL == "" {
		return ""
	}
	h := sha3.Sum256([]byte(dataURL))
	return fmt.Sprintf("%x", h[:])
}

func webProviderOrderSource(c *webImageCandidate) string {
	c = canonicalWebImageCandidate(c)
	switch {
	case c == nil:
		return "unknown"
	case c.authoritativeFinalOrderIndex >= 0:
		if c.authoritativeSource != "" {
			return c.authoritativeSource
		}
		return "authoritative"
	case c.directOrderIndex >= 0:
		return "direct_output"
	case c.fileIDOrderIndex >= 0:
		return "file_id"
	case c.sedimentIDOrderIndex >= 0:
		return "sediment_id"
	case c.downloadSuccessOrder > 0:
		return "download_success"
	default:
		return "unknown"
	}
}

func compareWebImageCandidate(a, b *webImageCandidate) bool {
	left := webImageCandidateSortTuple(a)
	right := webImageCandidateSortTuple(b)
	for i := range left {
		if left[i] == right[i] {
			continue
		}
		return left[i] < right[i]
	}
	return strings.Compare(firstString(a.rawURLs), firstString(b.rawURLs)) < 0
}

func webImageCandidateSortTuple(c *webImageCandidate) [6]int {
	if c == nil {
		return [6]int{maxSortInt, maxSortInt, maxSortInt, maxSortInt, maxSortInt, maxSortInt}
	}
	return [6]int{
		normalizeSortIndex(c.authoritativeFinalOrderIndex),
		normalizeSortIndex(c.directOrderIndex),
		normalizeSortIndex(c.fileIDOrderIndex),
		normalizeSortIndex(c.sedimentIDOrderIndex),
		normalizeSortIndex(c.firstSeenPollCount),
		normalizeSortIndex(c.downloadSuccessOrder),
	}
}

const maxSortInt = int(^uint(0) >> 1)

func normalizeSortIndex(v int) int {
	if v < 0 {
		return maxSortInt
	}
	return v
}

func isGeneratedWebAssetURL(rawURL string) bool {
	trimmed := strings.TrimSpace(rawURL)
	if strings.HasPrefix(trimmed, "/backend-api/files/download/") ||
		(strings.HasPrefix(trimmed, "/backend-api/conversation/") && strings.Contains(trimmed, "/attachment/") && strings.Contains(trimmed, "/download")) ||
		(strings.HasPrefix(trimmed, "/backend-api/estuary/content") && strings.Contains(trimmed, "id=file_")) {
		return true
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	path := strings.ToLower(u.EscapedPath())
	if strings.EqualFold(host, "chatgpt.com") && strings.HasPrefix(path, "/backend-api/") &&
		(strings.Contains(path, "/files/download/") ||
			(strings.Contains(path, "/attachment/") && strings.Contains(path, "/download")) ||
			(strings.Contains(path, "/estuary/content") && strings.Contains(strings.ToLower(u.RawQuery), "id=file_"))) {
		return true
	}
	if strings.Contains(host, "openaiassets.blob.core.windows.net") {
		return false
	}
	if strings.Contains(path, "/$web/chatgpt/") ||
		strings.Contains(path, "filled-plus-icon") ||
		strings.Contains(path, "icon") ||
		strings.Contains(path, "logo") {
		return false
	}
	return strings.Contains(host, "files.oaiusercontent.com") ||
		strings.Contains(host, "oaidalleapiprodscus.blob.core.windows.net") ||
		(strings.HasSuffix(host, ".blob.core.windows.net") && !strings.Contains(path, "/$web/"))
}

func firstString(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func sanitizeDiagURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return snippet([]byte(rawURL), 180)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func shouldUseWebDownloadHeaders(base, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "" && strings.HasPrefix(u.Path, "/backend-api/") {
		return true
	}
	if !strings.Contains(u.Path, "/backend-api/") {
		return false
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return strings.Contains(u.Host, "chatgpt.com")
	}
	return strings.EqualFold(u.Host, b.Host)
}

func logUpstream(ctx context.Context, req *provider.Request, entry provider.UpstreamLogEntry) {
	if req == nil || req.UpstreamLog == nil {
		return
	}
	if entry.Provider == "" {
		entry.Provider = "gpt"
	}
	req.UpstreamLog(ctx, entry)
}

func buildLegacyRequirementsToken(userAgent string) string {
	seed := fmt.Sprintf("%0.16f", rand.Float64())
	config := []any{
		3000 + rand.Intn(3)*1000,
		time.Now().In(time.FixedZone("EST", -5*3600)).Format("Mon Jan 02 2006 15:04:05") + " GMT-0500 (Eastern Standard Time)",
		4294705152,
		0,
		userAgent,
		"https://chatgpt.com/backend-api/sentinel/sdk.js",
		"",
		"en-US",
		"en-US,es-US,en,es",
		0,
		"webdriver≭false",
		"location",
		"window",
		float64(time.Now().UnixNano()) / 1e6,
		uuid.NewString(),
		"",
		16,
		float64(time.Now().UnixNano()) / 1e6,
	}
	answer, _ := powGenerate(seed, "0fffff", config)
	return "gAAAAAC" + answer
}

func buildProofToken(seed, difficulty, userAgent string) string {
	config := []any{
		3000 + rand.Intn(3)*1000,
		time.Now().In(time.FixedZone("EST", -5*3600)).Format("Mon Jan 02 2006 15:04:05") + " GMT-0500 (Eastern Standard Time)",
		4294705152,
		0,
		userAgent,
		"https://chatgpt.com/backend-api/sentinel/sdk.js",
		"",
		"en-US",
		"en-US,es-US,en,es",
		0,
		"webdriver≭false",
		"location",
		"window",
		float64(time.Now().UnixNano()) / 1e6,
		uuid.NewString(),
		"",
		16,
		float64(time.Now().UnixNano()) / 1e6,
	}
	answer, solved := powGenerate(seed, difficulty, config)
	if !solved {
		return "gAAAAAB" + base64.StdEncoding.EncodeToString([]byte(`"`+seed+`"`))
	}
	return "gAAAAAB" + answer
}

func powGenerate(seed, difficulty string, config []any) (string, bool) {
	target := difficulty
	diffBytes, err := hexToBytes(target)
	if err != nil || len(diffBytes) == 0 {
		return base64.StdEncoding.EncodeToString([]byte(`"` + seed + `"`)), false
	}
	static1 := mustJSON(config[:3])
	static1 = strings.TrimSuffix(static1, "]") + ","
	static2 := "," + strings.TrimPrefix(strings.TrimSuffix(mustJSON(config[4:9]), "]"), "[") + ","
	static3 := "," + strings.TrimPrefix(mustJSON(config[10:]), "[")
	seedBytes := []byte(seed)
	for i := 0; i < 500000; i++ {
		final := static1 + fmt.Sprint(i) + static2 + fmt.Sprint(i>>1) + static3
		encoded := base64.StdEncoding.EncodeToString([]byte(final))
		h := sha3.Sum512(append(seedBytes, []byte(encoded)...))
		if bytes.Compare(h[:len(diffBytes)], diffBytes) <= 0 {
			return encoded, true
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(`"` + seed + `"`)), false
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func hexToBytes(s string) ([]byte, error) {
	if len(s)%2 == 1 {
		s = "0" + s
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		var x byte
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			x <<= 4
			switch {
			case c >= '0' && c <= '9':
				x |= c - '0'
			case c >= 'a' && c <= 'f':
				x |= c - 'a' + 10
			case c >= 'A' && c <= 'F':
				x |= c - 'A' + 10
			default:
				return nil, fmt.Errorf("invalid hex")
			}
		}
		out[i] = x
	}
	return out, nil
}

func outputImagePayload(out responseOutputItem) (string, string) {
	if out.Result != "" {
		return out.Result, ""
	}
	if out.B64JSON != "" {
		return out.B64JSON, ""
	}
	if out.ImageB64 != "" {
		return out.ImageB64, ""
	}
	if out.URL != "" {
		return "", out.URL
	}
	for _, content := range out.Content {
		if content.Result != "" {
			return content.Result, ""
		}
		if content.B64JSON != "" {
			return content.B64JSON, ""
		}
		if content.ImageB64 != "" {
			return content.ImageB64, ""
		}
		if content.URL != "" {
			return "", content.URL
		}
	}
	return "", ""
}

func parseCompletedResponse(r io.Reader) (*responseCompletedEvent, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var dataLines []string
	var last *responseCompletedEvent
	var outputItems []responseOutputItem
	var partialItems []responseOutputItem
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return nil
		}
		var ev responseCompletedEvent
		err := json.Unmarshal([]byte(data), &ev)
		var direct struct {
			Output []responseOutputItem `json:"output"`
			Item   responseOutputItem   `json:"item"`
		}
		if err2 := json.Unmarshal([]byte(data), &direct); err2 == nil {
			if len(ev.Response.Output) == 0 && len(direct.Output) > 0 {
				ev.Type = "response.completed"
				ev.Response.Output = direct.Output
			}
			if direct.Item.Type != "" && ev.Type == "" {
				ev.Type = "response.output_item.done"
			}
		}
		if err != nil && len(ev.Response.Output) == 0 && direct.Item.Type == "" {
			return err
		}
		switch ev.Type {
		case "response.output_item.done":
			if direct.Item.Type != "" {
				outputItems = append(outputItems, direct.Item)
			}
		case "response.image_generation_call.partial_image":
			var partial struct {
				OutputFormat string `json:"output_format"`
				PartialB64   string `json:"partial_image_b64"`
			}
			if err := json.Unmarshal([]byte(data), &partial); err == nil && partial.PartialB64 != "" {
				partialItems = append(partialItems, responseOutputItem{
					Type:         "image_generation_call",
					Result:       partial.PartialB64,
					OutputFormat: partial.OutputFormat,
				})
			}
		}
		if ev.Type == "response.completed" || len(ev.Response.Output) > 0 || ev.Error != nil {
			last = &ev
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, fmt.Errorf("gpt image2 stream decode: %w", err)
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("gpt image2 stream read: %w", err)
	}
	if err := flush(); err != nil {
		return nil, fmt.Errorf("gpt image2 stream decode: %w", err)
	}
	if last == nil {
		last = &responseCompletedEvent{Type: "response.completed"}
	}
	if len(last.Response.Output) == 0 && len(outputItems) > 0 {
		last.Response.Output = outputItems
	}
	if len(last.Response.Output) == 0 && len(partialItems) > 0 {
		last.Response.Output = partialItems
	}
	return last, nil
}

func mimeForImageFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func parseSize(size string) (int, int) {
	if size == "" {
		return 1024, 1024
	}
	parts := strings.SplitN(size, "x", 2)
	if len(parts) != 2 {
		return 1024, 1024
	}
	var w, h int
	fmt.Sscanf(parts[0], "%d", &w)
	fmt.Sscanf(parts[1], "%d", &h)
	if w <= 0 {
		w = 1024
	}
	if h <= 0 {
		h = 1024
	}
	return w, h
}

func snippet(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	r := []rune(string(b))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "...(truncated)"
}
