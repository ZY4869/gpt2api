package chatgpt

import "testing"

func TestExtractImageToolMsgsAcceptsAssetPointerAndEstuaryURL(t *testing.T) {
	mapping := map[string]interface{}{
		"msg_1": map[string]interface{}{
			"message": map[string]interface{}{
				"create_time": 1.0,
				"author": map[string]interface{}{
					"role": "tool",
					"name": "image_generation",
				},
				"metadata": map[string]interface{}{
					"image_gen_title": "storyboard",
				},
				"content": map[string]interface{}{
					"content_type": "multimodal_text",
					"parts": []interface{}{
						map[string]interface{}{"asset_pointer": "file-service://file_asset_1"},
						"https://chatgpt.com/backend-api/estuary/content?id=file_estuary_2",
						"sediment://sed_3",
					},
				},
			},
		},
	}

	got := ExtractImageToolMsgs(mapping)
	if len(got) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(got))
	}
	if len(got[0].FileIDs) != 2 {
		t.Fatalf("file_ids = %#v, want 2 ids", got[0].FileIDs)
	}
	if got[0].FileIDs[0] != "file_asset_1" || got[0].FileIDs[1] != "file_estuary_2" {
		t.Fatalf("file_ids = %#v", got[0].FileIDs)
	}
	if len(got[0].SedimentIDs) != 1 || got[0].SedimentIDs[0] != "sed_3" {
		t.Fatalf("sediment_ids = %#v", got[0].SedimentIDs)
	}
}
