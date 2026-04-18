package gateway

import (
	"encoding/json"
	"testing"

	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func TestResponseInputToMessages(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		instructions string
		want         []chatgpt.ChatMessage
		wantErr      bool
	}{
		{
			name: "string_input",
			raw:  `"请画一只橘猫"`,
			want: []chatgpt.ChatMessage{{Role: "user", Content: "请画一只橘猫"}},
		},
		{
			name:         "object_input_with_instructions",
			raw:          `{"role":"user","content":[{"text":"画一张极简主义海报"}]}`,
			instructions: "请始终使用中文",
			want: []chatgpt.ChatMessage{
				{Role: "system", Content: "请始终使用中文"},
				{Role: "user", Content: "画一张极简主义海报"},
			},
		},
		{
			name: "array_input",
			raw:  `[{"role":"user","content":"第一句"},{"role":"assistant","content":[{"text":"第二句"}]},{"role":"user","content":{"text":"第三句"}}]`,
			want: []chatgpt.ChatMessage{
				{Role: "user", Content: "第一句"},
				{Role: "assistant", Content: "第二句"},
				{Role: "user", Content: "第三句"},
			},
		},
		{
			name:    "empty_input_error",
			raw:     `""`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs, err := responseInputToMessages(json.RawMessage(tc.raw), tc.instructions)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("responseInputToMessages error: %v", err)
			}
			if len(msgs) != len(tc.want) {
				t.Fatalf("message count = %d, want %d", len(msgs), len(tc.want))
			}
			for i := range tc.want {
				if msgs[i] != tc.want[i] {
					t.Fatalf("message[%d] = %+v, want %+v", i, msgs[i], tc.want[i])
				}
			}
		})
	}
}

func TestHasImageGenerationTool(t *testing.T) {
	if !hasImageGenerationTool([]ResponseToolDef{{Type: "image_generation"}}) {
		t.Fatal("expected image_generation tool to be detected")
	}
	if hasImageGenerationTool([]ResponseToolDef{{Type: "web_search"}}) {
		t.Fatal("did not expect non-image tool to match")
	}
}
