package gpt

import (
	"strings"
	"testing"
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
