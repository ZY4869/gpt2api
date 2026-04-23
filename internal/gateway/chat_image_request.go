package gateway

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

const defaultMixedModeMaxN = 10

type mixedModeRequestInput struct {
	Messages       []chatgpt.ChatMessage
	RequestedN     *int
	ThinkingEffort string
}

type mixedModePreparedRequest struct {
	Messages       []chatgpt.ChatMessage
	Prompt         string
	RequestedN     int
	ThinkingEffort string
}

var (
	reArabicImageCount  = regexp.MustCompile(`(?i)\b(10|[1-9])\s*(?:张|幅)(?:图|图片|插图|画面|场景)?(?:即可|就行|就可以)?`)
	reArabicSceneCount  = regexp.MustCompile(`(?i)\b(10|[1-9])\s*个\s*(?:图|图片|插图|画面|场景)`)
	reEnglishImageCount = regexp.MustCompile(`(?i)\b(10|[1-9])\s*(?:images?|pictures?|pics?)\b`)
	reChineseImageCount = regexp.MustCompile(`(十|两|一|二|三|四|五|六|七|八|九)\s*(?:张|幅)(?:图|图片|插图|画面|场景)?(?:即可|就行|就可以)?`)
	reChineseSceneCount = regexp.MustCompile(`(十|两|一|二|三|四|五|六|七|八|九)\s*个\s*(?:图|图片|插图|画面|场景)`)
)

func prepareMixedModeRequest(
	chatModel *modelpkg.Model,
	input mixedModeRequestInput,
	maxN int,
) (*mixedModePreparedRequest, *mixedModeAPIError) {
	prompt := extractLastUserPrompt(input.Messages)
	if strings.TrimSpace(prompt) == "" {
		return nil, &mixedModeAPIError{
			Status:  400,
			Code:    "invalid_request_error",
			Message: "image_generation=true 时,最后一条 user 消息必须提供生图提示词",
		}
	}

	if maxN <= 0 {
		maxN = defaultMixedModeMaxN
	}
	if maxN > defaultMixedModeMaxN {
		maxN = defaultMixedModeMaxN
	}

	isThinking := isThinkingModel(chatModel)
	effort := strings.TrimSpace(input.ThinkingEffort)
	if effort != "" && !isThinking {
		return nil, &mixedModeAPIError{
			Status:  400,
			Code:    "invalid_request_error",
			Message: "thinking_effort 仅允许 thinking 模型在 image_generation 模式下使用",
		}
	}
	if isThinking && effort == "" {
		effort = "standard"
	}

	explicitCount, explicitFound := extractPromptImageCount(prompt)
	requestedN := 1
	if explicitFound {
		requestedN = explicitCount
	}
	if input.RequestedN != nil {
		if *input.RequestedN <= 0 {
			return nil, &mixedModeAPIError{
				Status:  400,
				Code:    "invalid_request_error",
				Message: "n 必须是大于 0 的整数",
			}
		}
		requestedN = *input.RequestedN
		if explicitFound && explicitCount != requestedN {
			return nil, &mixedModeAPIError{
				Status:  400,
				Code:    "invalid_request_error",
				Message: fmt.Sprintf("prompt 中已明确要求生成 %d 张图片,与参数 n=%d 冲突", explicitCount, requestedN),
			}
		}
	}
	if requestedN <= 0 || requestedN > maxN {
		return nil, &mixedModeAPIError{
			Status:  400,
			Code:    "invalid_request_error",
			Message: fmt.Sprintf("n 必须在 1 到 %d 之间", maxN),
		}
	}

	compiledPrompt := maybeAppendClaritySuffix(buildMixedModePrompt(prompt, requestedN))
	return &mixedModePreparedRequest{
		Messages:       cloneMessagesForImageTool(input.Messages, compiledPrompt),
		Prompt:         compiledPrompt,
		RequestedN:     requestedN,
		ThinkingEffort: effort,
	}, nil
}

func buildMixedModePrompt(prompt string, n int) string {
	prompt = strings.TrimSpace(prompt)
	if n <= 1 {
		return prompt + "\n\nReturn exactly 1 separate image. Preserve any user-specified transparent background, aspect ratio, composition, and style requirements."
	}
	return fmt.Sprintf(
		"%s\n\nReturn exactly %d separate images in a single generation request. The images must form one continuous story sequence with the same main character, the same visual style, and coherent scene progression. Each image must show a distinct and reasonable action in order. Preserve any user-specified transparent background, aspect ratio, composition, and style requirements. Do not collapse multiple scenes into a single image.",
		prompt,
		n,
	)
}

func extractPromptImageCount(prompt string) (int, bool) {
	for _, re := range []*regexp.Regexp{reArabicImageCount, reArabicSceneCount, reEnglishImageCount} {
		match := re.FindStringSubmatch(prompt)
		if len(match) == 2 {
			n, err := strconv.Atoi(match[1])
			if err == nil {
				return n, true
			}
		}
	}
	for _, re := range []*regexp.Regexp{reChineseImageCount, reChineseSceneCount} {
		match := re.FindStringSubmatch(prompt)
		if len(match) != 2 {
			continue
		}
		n, ok := map[string]int{
			"一": 1,
			"二": 2,
			"两": 2,
			"三": 3,
			"四": 4,
			"五": 5,
			"六": 6,
			"七": 7,
			"八": 8,
			"九": 9,
			"十": 10,
		}[match[1]]
		if ok {
			return n, true
		}
	}
	return 0, false
}

func cloneMessagesForImageTool(messages []chatgpt.ChatMessage, prompt string) []chatgpt.ChatMessage {
	out := make([]chatgpt.ChatMessage, len(messages))
	copy(out, messages)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" && strings.TrimSpace(out[i].Content) != "" {
			out[i].Content = prompt
			break
		}
	}
	return out
}
