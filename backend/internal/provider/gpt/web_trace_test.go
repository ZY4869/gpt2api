package gpt

import (
	"strings"
	"testing"

	"github.com/kleinai/backend/internal/provider"
)

func TestExtractWebImageToolIDs(t *testing.T) {
	raw := `{
		"conversation_id":"conv_123",
		"messages":[
			{
				"message":{
					"author":{"role":"tool"},
					"metadata":{"async_task_type":"image_gen"},
					"content":{
						"content_type":"multimodal_text",
						"parts":[
							{"asset_pointer":"file-service://file_abc12345"},
							{"asset_pointer":"sediment://sed_xyz98765"},
							"file-service://file_def67890"
						]
					}
				}
			}
		]
	}`
	fileIDs, sedimentIDs := extractWebImageToolIDs([]byte(raw))
	if len(fileIDs) != 2 {
		t.Fatalf("expected 2 file ids, got %v", fileIDs)
	}
	if len(sedimentIDs) != 1 || sedimentIDs[0] != "sed_xyz98765" {
		t.Fatalf("expected sediment id, got %v", sedimentIDs)
	}
}

func TestParseWebImageSSE(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		`data: {"type":"server_ste_metadata","conversation_id":"conv_456","metadata":{"tool_invoked":true,"turn_use_case":"image"}}`,
		"",
		`data: {"type":"response.completed","response":{"output":[{"type":"image_generation_call","result":"ZmFrZS1iNjQ=","output_format":"png"}]}}`,
		"",
	}, "\n"))

	conversationID, fileIDs, sedimentIDs, directURLs, lastText, err := parseWebImageSSE(raw)
	if err != nil {
		t.Fatalf("parseWebImageSSE error: %v", err)
	}
	if conversationID != "conv_456" {
		t.Fatalf("unexpected conversation id: %s", conversationID)
	}
	if len(fileIDs) != 0 || len(sedimentIDs) != 0 {
		t.Fatalf("unexpected ids: file=%v sediment=%v", fileIDs, sedimentIDs)
	}
	if len(directURLs) != 1 {
		t.Fatalf("expected 1 direct url, got %v", directURLs)
	}
	if lastText != "" {
		t.Fatalf("unexpected text: %q", lastText)
	}
}

func TestParseWebImageSSEIgnoresUploadedReferenceIDs(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		`data: {"conversation_id":"conv_ref","message":{"author":{"role":"user"},"content":{"content_type":"multimodal_text","parts":[{"asset_pointer":"file-service://file_reference12345"},"make it transparent"]}}}`,
		"",
	}, "\n"))

	conversationID, fileIDs, sedimentIDs, directURLs, _, err := parseWebImageSSE(raw)
	if err != nil {
		t.Fatalf("parseWebImageSSE error: %v", err)
	}
	if conversationID != "conv_ref" {
		t.Fatalf("unexpected conversation id: %s", conversationID)
	}
	if len(fileIDs) != 0 || len(sedimentIDs) != 0 || len(directURLs) != 0 {
		t.Fatalf("reference image should not be treated as output: file=%v sediment=%v urls=%v", fileIDs, sedimentIDs, directURLs)
	}
}

func TestExtractWebImageToolIDsAcceptsAssistantImageMessages(t *testing.T) {
	raw := []byte(`{
		"conversation_id":"conv_assistant",
		"messages":[
			{
				"message":{
					"author":{"role":"assistant"},
					"metadata":{"async_task_type":"image_generation"},
					"content":{
						"content_type":"multimodal_text",
						"parts":[
							{"kind":"text","text":"done"},
							{"nested":{"asset_pointer":"file-service://file_out123456"}},
							["sediment://sed_out987654"]
						]
					}
				}
			}
		]
	}`)
	fileIDs, sedimentIDs := extractWebImageToolIDs(raw)
	if len(fileIDs) != 1 || fileIDs[0] != "file_out123456" {
		t.Fatalf("expected assistant image file id, got %v", fileIDs)
	}
	if len(sedimentIDs) != 1 || sedimentIDs[0] != "sed_out987654" {
		t.Fatalf("expected assistant sediment id, got %v", sedimentIDs)
	}
}

func TestExtractWebImageToolIDsAcceptsMetadataAttachments(t *testing.T) {
	raw := []byte(`{
		"conversation_id":"conv_attach",
		"messages":[
			{
				"message":{
					"author":{"role":"assistant"},
					"metadata":{
						"async_task_type":"image_generation",
						"attachments":[
							{"id":"file_meta123456"},
							{"file_id":"file_meta789012"}
						]
					},
					"content":{
						"content_type":"text",
						"parts":["done"]
					}
				}
			}
		]
	}`)
	fileIDs, sedimentIDs := extractWebImageToolIDs(raw)
	if len(fileIDs) != 2 || fileIDs[0] != "file_meta123456" || fileIDs[1] != "file_meta789012" {
		t.Fatalf("expected attachment file ids, got %v", fileIDs)
	}
	if len(sedimentIDs) != 0 {
		t.Fatalf("unexpected sediment ids: %v", sedimentIDs)
	}
}

func TestExtractWebImageToolIDsAcceptsNestedMetadataReferences(t *testing.T) {
	raw := []byte(`{
		"conversation_id":"conv_nested_meta",
		"messages":[
			{
				"message":{
					"author":{"role":"assistant"},
					"metadata":{
						"async_task_type":"image_generation",
						"content_references_by_file":{
							"file_1":[
								{
									"type":"image_inline",
									"ref_id":"file_meta_ref111111",
									"download_url":"\/backend-api\/files\/download\/file_meta_ref111111?conversation_id=conv_nested_meta&inline=false"
								}
							]
						},
						"conversation_context_citation_metadata":[
							{
								"asset_pointer":"file-service://file_meta_ref222222"
							},
							{
								"attachment":{
									"file_id":"file_meta_ref333333"
								}
							},
							{
								"asset_pointer":"sediment://sed_meta_ref444444"
							}
						]
					},
					"content":{
						"content_type":"text",
						"parts":["done"]
					}
				}
			}
		]
	}`)
	fileIDs, sedimentIDs := extractWebImageToolIDs(raw)
	if len(fileIDs) != 3 {
		t.Fatalf("expected 3 nested metadata file ids, got %v", fileIDs)
	}
	if fileIDs[0] != "file_meta_ref111111" || fileIDs[1] != "file_meta_ref222222" || fileIDs[2] != "file_meta_ref333333" {
		t.Fatalf("unexpected nested metadata file ids: %v", fileIDs)
	}
	if len(sedimentIDs) != 1 || sedimentIDs[0] != "sed_meta_ref444444" {
		t.Fatalf("unexpected nested metadata sediment ids: %v", sedimentIDs)
	}
}

func TestParseWebImageSSECollectsRawFileIDs(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		`data: {"conversation_id":"conv_raw_file","message":{"author":{"role":"assistant"},"metadata":{"async_task_type":"image_generation","content_references_by_file":{"k":[{"type":"image_inline","ref_id":"file_raw111111"}]}},"content":{"content_type":"multimodal_text","parts":[]}}}`,
		"",
		`data: {"download_url":"\/backend-api\/files\/download\/file_raw111111?conversation_id=conv_raw_file&inline=false"}`,
		"",
	}, "\n"))

	conversationID, fileIDs, sedimentIDs, directURLs, lastText, err := parseWebImageSSE(raw)
	if err != nil {
		t.Fatalf("parseWebImageSSE error: %v", err)
	}
	if conversationID != "conv_raw_file" {
		t.Fatalf("unexpected conversation id: %s", conversationID)
	}
	if len(fileIDs) != 1 || fileIDs[0] != "file_raw111111" {
		t.Fatalf("expected raw file id from metadata, got %v", fileIDs)
	}
	if len(sedimentIDs) != 0 {
		t.Fatalf("unexpected sediment ids: %v", sedimentIDs)
	}
	if len(directURLs) != 1 || !strings.Contains(directURLs[0], "file_raw111111") {
		t.Fatalf("expected raw download url, got %v", directURLs)
	}
	if lastText != "" {
		t.Fatalf("unexpected text: %q", lastText)
	}
}

func TestWebImageMessageContentReferenceOrder(t *testing.T) {
	content, metadata := webImageMessageContent("make it transparent", []webUploadMeta{{
		FileID:        "file_ref123456",
		LibraryFileID: "libfile_ref123456",
		FileName:      "image_1.png",
		Mime:          "image/png",
		FileSize:      1234,
		Width:         512,
		Height:        512,
	}})

	if content["content_type"] != "multimodal_text" {
		t.Fatalf("unexpected content type: %v", content["content_type"])
	}
	parts, ok := content["parts"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("unexpected parts: %#v", content["parts"])
	}
	ref, ok := parts[0].(map[string]any)
	if !ok || ref["asset_pointer"] != "sediment://file_ref123456" {
		t.Fatalf("reference image should be first, got %#v", parts[0])
	}
	if parts[1] != "make it transparent" {
		t.Fatalf("prompt should be last, got %#v", parts[1])
	}
	attachments, ok := metadata["attachments"].([]map[string]any)
	if !ok || len(attachments) != 1 || attachments[0]["id"] != "file_ref123456" {
		t.Fatalf("unexpected attachments: %#v", metadata["attachments"])
	}
	if attachments[0]["source"] != "library" || attachments[0]["library_file_id"] != "libfile_ref123456" {
		t.Fatalf("unexpected attachment metadata: %#v", attachments[0])
	}
}

func TestExtractWebImageDirectURLsIgnoresChatGPTStaticAssets(t *testing.T) {
	raw := `{
		"url":"https://openaiassets.blob.core.windows.net/$web/chatgpt/filled-plus-icon.png",
		"image":"https://files.oaiusercontent.com/file-real-output.png?se=1"
	}`
	urls := extractWebImageDirectURLs(raw)
	if len(urls) != 1 || !strings.Contains(urls[0], "files.oaiusercontent.com") {
		t.Fatalf("expected only generated asset URL, got %#v", urls)
	}
}

func TestExtractWebImageDirectURLsAcceptsRelativeBackendDownloadPaths(t *testing.T) {
	raw := `{
		"download_url":"\/backend-api\/files\/download\/file_abc123xyz?conversation_id=conv_123&inline=false",
		"attachment_url":"\/backend-api\/conversation\/conv_123\/attachment\/sed_456xyz\/download",
		"estuary_url":"\/backend-api\/estuary\/content?id=file_est123xyz&ts=1&p=fs"
	}`
	urls := extractWebImageDirectURLs(raw)
	if len(urls) != 3 {
		t.Fatalf("expected 3 backend download urls, got %#v", urls)
	}
	if urls[0] != "/backend-api/files/download/file_abc123xyz?conversation_id=conv_123&inline=false" {
		t.Fatalf("unexpected first backend url %#v", urls[0])
	}
	if urls[1] != "/backend-api/conversation/conv_123/attachment/sed_456xyz/download" {
		t.Fatalf("unexpected second backend url %#v", urls[1])
	}
	if urls[2] != "/backend-api/estuary/content?id=file_est123xyz&ts=1&p=fs" {
		t.Fatalf("unexpected third backend url %#v", urls[2])
	}
}

func TestAppendUniqueWebAssetSkipsDuplicateDataURL(t *testing.T) {
	seen := map[string]bool{}
	assets := make([]provider.Asset, 0, 2)

	var added bool
	assets, added = appendUniqueWebAsset(assets, seen, provider.Asset{URL: "data:image/png;base64,AAA", Width: 1024, Height: 1024})
	if !added || len(assets) != 1 {
		t.Fatalf("expected first asset to be appended, got added=%v len=%d", added, len(assets))
	}
	assets, added = appendUniqueWebAsset(assets, seen, provider.Asset{URL: "data:image/png;base64,AAA", Width: 1024, Height: 1024})
	if added || len(assets) != 1 {
		t.Fatalf("expected duplicate asset to be skipped, got added=%v len=%d", added, len(assets))
	}
	assets, added = appendUniqueWebAsset(assets, seen, provider.Asset{URL: "data:image/png;base64,BBB", Width: 1024, Height: 1024})
	if !added || len(assets) != 2 {
		t.Fatalf("expected distinct asset to be appended, got added=%v len=%d", added, len(assets))
	}
}

func TestMergeOrderedWebAssetURLsPrefersDirectOrder(t *testing.T) {
	got := mergeOrderedWebAssetURLs(
		[]string{
			"/backend-api/files/download/file_direct_2?conversation_id=conv_1&inline=false",
			"/backend-api/files/download/file_direct_1?conversation_id=conv_1&inline=false",
		},
		[]string{
			"/backend-api/files/download/file_direct_1?conversation_id=conv_1&inline=false",
			"/backend-api/files/download/file_direct_2?conversation_id=conv_1&inline=false",
			"/backend-api/files/download/file_poll_3?conversation_id=conv_1&inline=false",
		},
	)
	if len(got) != 3 {
		t.Fatalf("expected 3 urls, got %#v", got)
	}
	if !strings.Contains(got[0], "file_direct_2") || !strings.Contains(got[1], "file_direct_1") || !strings.Contains(got[2], "file_poll_3") {
		t.Fatalf("unexpected merged order %#v", got)
	}
}

func TestParseWebImageSSEPreservesDirectURLOrder(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		`data: {"conversation_id":"conv_order","first":"\/backend-api\/files\/download\/file_raw_b?conversation_id=conv_order&inline=false"}`,
		"",
		`data: {"conversation_id":"conv_order","second":"\/backend-api\/files\/download\/file_raw_a?conversation_id=conv_order&inline=false"}`,
		"",
	}, "\n"))

	_, fileIDs, sedimentIDs, directURLs, _, err := parseWebImageSSE(raw)
	if err != nil {
		t.Fatalf("parseWebImageSSE error: %v", err)
	}
	if len(fileIDs) != 0 || len(sedimentIDs) != 0 {
		t.Fatalf("expected no file/sediment ids, got file=%v sediment=%v", fileIDs, sedimentIDs)
	}
	if len(directURLs) != 2 || !strings.Contains(directURLs[0], "file_raw_b") || !strings.Contains(directURLs[1], "file_raw_a") {
		t.Fatalf("expected direct url order to be preserved, got %#v", directURLs)
	}
}

func TestBuildOrderedWebAssetsPrefersDirectOrderOverResolvedOrder(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidate(pool, "direct-a")
	a.directOrderIndex = 1
	a.fileIDOrderIndex = 0
	a.sedimentIDOrderIndex = -1
	a.firstSeenPollCount = 1
	a.downloadSuccessOrder = 1
	a.dataURL = "data:image/png;base64,AAA"
	a.mime = "image/png"
	b := ensureWebImageCandidate(pool, "direct-b")
	b.directOrderIndex = 0
	b.fileIDOrderIndex = 1
	b.sedimentIDOrderIndex = -1
	b.firstSeenPollCount = 1
	b.downloadSuccessOrder = 2
	b.dataURL = "data:image/png;base64,BBB"
	b.mime = "image/png"

	assets := buildOrderedWebAssets(pool, 10, 1024, 1024, "1:1")
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
	if assets[0].URL != "data:image/png;base64,BBB" || assets[1].URL != "data:image/png;base64,AAA" {
		t.Fatalf("expected direct-order result, got %#v", assets)
	}
}

func TestBuildOrderedWebAssetsFallsBackToResolvedOrderWhenNoDirectOrder(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidate(pool, "resolved-1")
	a.directOrderIndex = -1
	a.fileIDOrderIndex = 0
	a.sedimentIDOrderIndex = -1
	a.firstSeenPollCount = 2
	a.downloadSuccessOrder = 2
	a.dataURL = "data:image/png;base64,AAA"
	a.mime = "image/png"
	b := ensureWebImageCandidate(pool, "resolved-2")
	b.directOrderIndex = -1
	b.fileIDOrderIndex = 1
	b.sedimentIDOrderIndex = -1
	b.firstSeenPollCount = 1
	b.downloadSuccessOrder = 1
	b.dataURL = "data:image/png;base64,BBB"
	b.mime = "image/png"

	assets := buildOrderedWebAssets(pool, 10, 1024, 1024, "1:1")
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
	if assets[0].URL != "data:image/png;base64,AAA" || assets[1].URL != "data:image/png;base64,BBB" {
		t.Fatalf("expected resolved-order fallback, got %#v", assets)
	}
}

func TestBuildOrderedWebAssetsDoesNotUseDownloadOrderWhenDirectOrderExists(t *testing.T) {
	pool := newWebImageCandidatePool()
	first := ensureWebImageCandidate(pool, "later-downloaded-but-first")
	first.directOrderIndex = 0
	first.fileIDOrderIndex = 1
	first.sedimentIDOrderIndex = -1
	first.firstSeenPollCount = 1
	first.downloadSuccessOrder = 2
	first.dataURL = "data:image/png;base64,FIRST"
	first.mime = "image/png"
	second := ensureWebImageCandidate(pool, "earlier-downloaded-but-second")
	second.directOrderIndex = 1
	second.fileIDOrderIndex = 0
	second.sedimentIDOrderIndex = -1
	second.firstSeenPollCount = 1
	second.downloadSuccessOrder = 1
	second.dataURL = "data:image/png;base64,SECOND"
	second.mime = "image/png"

	assets := buildOrderedWebAssets(pool, 10, 1024, 1024, "1:1")
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
	if assets[0].URL != "data:image/png;base64,FIRST" || assets[1].URL != "data:image/png;base64,SECOND" {
		t.Fatalf("expected direct order to beat download order, got %#v", assets)
	}
}

func TestBuildOrderedWebAssetsKeepsSingleCandidateForDuplicateSource(t *testing.T) {
	pool := newWebImageCandidatePool()
	state := webConversationImageState{
		DirectURLs: []string{"/backend-api/files/download/file_dup"},
		FileIDs:    []string{"file_dup"},
	}
	candidate := ensureWebImageCandidate(pool, "/backend-api/files/download/file_dup")
	updateWebImageCandidateOrder(candidate, state, 0, 1)
	candidate.dataURL = "data:image/png;base64,DUP"
	candidate.mime = "image/png"

	duplicate := ensureWebImageCandidate(pool, "/backend-api/files/download/file_dup")
	updateWebImageCandidateOrder(duplicate, state, 0, 2)

	assets := buildOrderedWebAssets(pool, 10, 1024, 1024, "1:1")
	if len(assets) != 1 {
		t.Fatalf("expected duplicate source to collapse into one asset, got %d", len(assets))
	}
	if assets[0].URL != "data:image/png;base64,DUP" {
		t.Fatalf("unexpected asset %#v", assets[0])
	}
}

func TestBuildOrderedWebAssetsAuthoritativeOrderBeatsDirectOrder(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_a", Source: "metadata.attachments"})
	a.directOrderIndex = 1
	a.fileIDOrderIndex = 0
	a.firstSeenPollCount = 1
	a.downloadSuccessOrder = 1
	a.dataURL = "data:image/png;base64,AAA"
	a.mime = "image/png"
	b := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_b", Source: "metadata.attachments"})
	b.directOrderIndex = 0
	b.fileIDOrderIndex = 1
	b.firstSeenPollCount = 1
	b.downloadSuccessOrder = 2
	b.dataURL = "data:image/png;base64,BBB"
	b.mime = "image/png"
	webApplyAuthoritativeOrder(pool, []webOrderedRef{
		{FileID: "file_a", Source: "metadata.attachments"},
		{FileID: "file_b", Source: "metadata.attachments"},
	})

	assets := buildOrderedWebAssets(pool, 10, 1024, 1024, "1:1")
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
	if assets[0].URL != "data:image/png;base64,AAA" || assets[1].URL != "data:image/png;base64,BBB" {
		t.Fatalf("expected authoritative-order result, got %#v", assets)
	}
}

func TestMergeWebConversationImageStateReplacesAuthoritativeSnapshot(t *testing.T) {
	base := webConversationImageState{
		OrderedRefs: []webOrderedRef{
			{FileID: "file_b", Source: "metadata.attachments"},
			{FileID: "file_a", Source: "metadata.attachments"},
		},
		FileIDs:               []string{"file_b", "file_a"},
		HasAuthoritativeOrder: true,
	}
	extra := webConversationImageState{
		OrderedRefs: []webOrderedRef{
			{FileID: "file_a", Source: "metadata.attachments"},
			{FileID: "file_b", Source: "metadata.attachments"},
		},
		FileIDs:               []string{"file_a", "file_b"},
		HasAuthoritativeOrder: true,
	}

	merged := mergeWebConversationImageState(base, extra)
	if len(merged.OrderedRefs) != 2 {
		t.Fatalf("expected 2 ordered refs, got %#v", merged.OrderedRefs)
	}
	if merged.OrderedRefs[0].FileID != "file_a" || merged.OrderedRefs[1].FileID != "file_b" {
		t.Fatalf("expected latest authoritative snapshot to replace prior order, got %#v", merged.OrderedRefs)
	}
	if len(merged.FileIDs) != 2 || merged.FileIDs[0] != "file_b" || merged.FileIDs[1] != "file_a" {
		t.Fatalf("expected non-authoritative file id order to remain first-seen stable, got %#v", merged.FileIDs)
	}
}

func TestExtractWebAuthoritativeOrderedRefsPrefersAttachmentArrayOrder(t *testing.T) {
	raw := []byte(`{
		"messages":[
			{
				"message":{
					"author":{"role":"assistant"},
					"metadata":{
						"async_task_type":"image_generation",
						"attachments":[
							{"file_id":"file_b"},
							{"file_id":"file_a"}
						],
						"content_references_by_file":{
							"x":[
								{"ref_id":"file_a"},
								{"ref_id":"file_b"}
							]
						}
					},
					"content":{"content_type":"multimodal_text","parts":["done"]}
				}
			}
		]
	}`)
	refs, ok := extractWebAuthoritativeOrderedRefs(raw, nil)
	if !ok {
		t.Fatalf("expected authoritative refs")
	}
	if len(refs) != 2 || refs[0].FileID != "file_b" || refs[1].FileID != "file_a" {
		t.Fatalf("expected attachment array order, got %#v", refs)
	}
}

func TestExtractWebAuthoritativeOrderedRefsSkipsUploadedRefs(t *testing.T) {
	raw := []byte(`{
		"messages":[
			{
				"message":{
					"author":{"role":"assistant"},
					"metadata":{
						"async_task_type":"image_generation",
						"attachments":[
							{"file_id":"file_ref"},
							{"file_id":"file_out"}
						]
					},
					"content":{"content_type":"multimodal_text","parts":["done"]}
				}
			}
		]
	}`)
	refs, ok := extractWebAuthoritativeOrderedRefs(raw, []webUploadMeta{{FileID: "file_ref"}})
	if !ok {
		t.Fatalf("expected authoritative refs")
	}
	if len(refs) != 1 || refs[0].FileID != "file_out" {
		t.Fatalf("expected uploaded ref to be excluded, got %#v", refs)
	}
}

func TestWebImageModelSlugDefaultsToThinking(t *testing.T) {
	if got := webImageModelSlug(nil); got != defaultWebImageThinkingModel {
		t.Fatalf("expected default thinking model %q, got %q", defaultWebImageThinkingModel, got)
	}
}

func TestWebImageModelSlugRejectsNonThinkingOverride(t *testing.T) {
	req := &provider.Request{
		Params: map[string]any{
			"web_model": "gpt-4o",
		},
	}
	if got := webImageModelSlug(req); got != defaultWebImageThinkingModel {
		t.Fatalf("expected non-thinking override to fall back to %q, got %q", defaultWebImageThinkingModel, got)
	}
}

func TestWebImageModelSlugAcceptsThinkingOverride(t *testing.T) {
	req := &provider.Request{
		Params: map[string]any{
			"web_model": "gpt-5-thinking-pro",
		},
	}
	if got := webImageModelSlug(req); got != "gpt-5-thinking-pro" {
		t.Fatalf("expected thinking override to be preserved, got %q", got)
	}
}

func TestWebImageConversationPlanUsesSingleConversationForMultiImage(t *testing.T) {
	limit, requireCompleteSet := webImageConversationPlan(10)
	if limit != 1 {
		t.Fatalf("expected single conversation limit, got %d", limit)
	}
	if !requireCompleteSet {
		t.Fatalf("expected multi-image plan to require complete set")
	}
}

func TestWebImageConversationPlanSingleImageIsNonStrict(t *testing.T) {
	limit, requireCompleteSet := webImageConversationPlan(1)
	if limit != 1 {
		t.Fatalf("expected single-image conversation limit 1, got %d", limit)
	}
	if requireCompleteSet {
		t.Fatalf("did not expect single-image plan to require complete set")
	}
}

func TestWebImagePromptV2IncludesSingleConversationDirectiveForSetGeneration(t *testing.T) {
	got := webImagePromptV2("做一个 1-10 儿童插画套图", "4:3", "1152x864", 10)
	if !strings.Contains(got, "同一个对话回复中一次性生成 10 张") {
		t.Fatalf("expected single-conversation directive in prompt, got %q", got)
	}
	if !strings.Contains(got, "将宽高比设为 4:3") {
		t.Fatalf("expected ratio directive in prompt, got %q", got)
	}
}

func TestWebImageTestModeRequiresGPTWebMultiImage(t *testing.T) {
	req := &provider.Request{
		ModelCode: "gpt-image-2",
		Count:     10,
		Params:    map[string]any{"web_test_mode": "wait_all_then_download"},
	}
	mode := webImageTestMode(req)
	if !mode.Enabled {
		t.Fatalf("expected test mode to be enabled")
	}
	if !mode.DownloadDeferred || !mode.StrictFailOnIncomplete {
		t.Fatalf("unexpected test mode state %#v", mode)
	}
}

func TestWebImageTestModeIgnoresSingleImageRequests(t *testing.T) {
	req := &provider.Request{
		ModelCode: "gpt-image-2",
		Count:     1,
		Params:    map[string]any{"web_test_mode": "wait_all_then_download"},
	}
	if mode := webImageTestMode(req); mode.Enabled {
		t.Fatalf("expected single-image request to ignore test mode")
	}
}

func TestWebUpdateCandidatePoolFromResolvedURLsDoesNotDownload(t *testing.T) {
	pool := newWebImageCandidatePool()
	state := webConversationImageState{
		DirectURLs: []string{"/backend-api/files/download/file_a"},
		FileIDs:    []string{"file_a"},
	}
	webUpdateCandidatePoolFromResolvedURLs(pool, state, []string{"/backend-api/files/download/file_a"}, 1)
	if countWebImageCandidates(pool) != 1 {
		t.Fatalf("expected one candidate")
	}
	if countWebImageCandidatesWithData(pool) != 0 {
		t.Fatalf("expected deferred collection to avoid downloading data")
	}
}

func TestWebAuthoritativeStableRoundsRequiresTwoMatchingSnapshots(t *testing.T) {
	state := webConversationImageState{
		OrderedRefs: []webOrderedRef{
			{FileID: "file_1", Source: "metadata.attachments"},
			{FileID: "file_2", Source: "metadata.attachments"},
		},
		HasAuthoritativeOrder: true,
	}
	snapshot, rounds := webAuthoritativeStableRounds(state, 2, "", 0)
	if snapshot == "" || rounds != 1 {
		t.Fatalf("expected first snapshot to start stability tracking, got snapshot=%q rounds=%d", snapshot, rounds)
	}
	snapshot, rounds = webAuthoritativeStableRounds(state, 2, snapshot, rounds)
	if rounds != 2 {
		t.Fatalf("expected second identical snapshot to mark stable, got %d", rounds)
	}
}

func TestBuildFinalOrderedWebCandidatesUsesAuthoritativeOrder(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_a", Source: "metadata.attachments"})
	b := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_b", Source: "metadata.attachments"})
	a.directOrderIndex = 1
	b.directOrderIndex = 0
	webApplyAuthoritativeOrder(pool, []webOrderedRef{
		{FileID: "file_a", Source: "metadata.attachments"},
		{FileID: "file_b", Source: "metadata.attachments"},
	})
	got := buildFinalOrderedWebCandidates(pool, 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
	if got[0].fileID != "file_a" || got[1].fileID != "file_b" {
		t.Fatalf("expected authoritative order, got %#v %#v", got[0], got[1])
	}
}

func TestWebCurrentOrderStableRoundsRequiresTwoMatchingSnapshots(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_a"})
	b := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_b"})
	a.directOrderIndex = 0
	b.directOrderIndex = 1

	snapshot, rounds := webCurrentOrderStableRounds(pool, 2, "", 0)
	if snapshot == "" || rounds != 1 {
		t.Fatalf("expected first current-order snapshot, got snapshot=%q rounds=%d", snapshot, rounds)
	}
	snapshot, rounds = webCurrentOrderStableRounds(pool, 2, snapshot, rounds)
	if rounds != 2 {
		t.Fatalf("expected second identical current-order snapshot to be stable, got %d", rounds)
	}
}

func TestResolveWebImageTestModeFinalCandidatesFallsBackToCurrentStable(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_a"})
	b := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_b"})
	a.directOrderIndex = 0
	b.directOrderIndex = 1

	snapshot := webCandidateOrderSnapshotKey(pool, 2)
	got, strategy := resolveWebImageTestModeFinalCandidates(pool, 2, false, 0, snapshot, 2, "")
	if strategy != "current_stable" {
		t.Fatalf("expected current_stable strategy, got %q", strategy)
	}
	if len(got) != 2 || got[0].fileID != "file_a" || got[1].fileID != "file_b" {
		t.Fatalf("unexpected fallback order: %#v", got)
	}
}

func TestResolveWebImageTestModeFinalCandidatesFallsBackToFirstComplete(t *testing.T) {
	pool := newWebImageCandidatePool()
	a := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_a"})
	b := ensureWebImageCandidateForOrderedRef(pool, webOrderedRef{FileID: "file_b"})
	a.directOrderIndex = 1
	b.directOrderIndex = 0

	got, strategy := resolveWebImageTestModeFinalCandidates(pool, 2, false, 0, "", 0, "file:file_b|file:file_a")
	if strategy != "first_complete" {
		t.Fatalf("expected first_complete strategy, got %q", strategy)
	}
	if len(got) != 2 || got[0].fileID != "file_b" || got[1].fileID != "file_a" {
		t.Fatalf("unexpected first-complete order: %#v", got)
	}
}

func TestFirstWebImageDownloadURLPrefersFileIDThenRawURL(t *testing.T) {
	candidate := &webImageCandidate{
		fileID:  "file_123",
		rawURLs: []string{"/backend-api/files/download/file_999"},
	}
	if got := firstWebImageDownloadURL(candidate); got != "/backend-api/files/download/file_123" {
		t.Fatalf("expected file-id download URL, got %q", got)
	}
	candidate.fileID = ""
	if got := firstWebImageDownloadURL(candidate); got != "/backend-api/files/download/file_999" {
		t.Fatalf("expected fallback raw URL, got %q", got)
	}
}
