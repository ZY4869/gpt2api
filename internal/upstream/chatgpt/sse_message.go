package chatgpt

import (
	"encoding/json"
	"strings"
)

type StreamMessageUpdate struct {
	AssistantDelta string
	ReasoningDelta string
	AssistantText  string
	ReasoningText  string
	ConversationID string
	FileIDs        []string
	SedimentIDs    []string
	FinishType     string
	ImageGenTaskID string
	AsyncAccepted  bool
	Final          bool
}

type StreamMessageCollector struct {
	textCollector  *sseTextCollector
	conversationID string
	imageGenTaskID string
	finishType     string
	fileIDs        []string
	sedimentIDs    []string
	seenFileIDs    map[string]struct{}
	seenSediment   map[string]struct{}
	asyncAccepted  bool
}

func NewStreamMessageCollector() *StreamMessageCollector {
	return &StreamMessageCollector{
		textCollector: newSSETextCollector(),
		seenFileIDs:   map[string]struct{}{},
		seenSediment:  map[string]struct{}{},
	}
}

func (c *StreamMessageCollector) Consume(data []byte) StreamMessageUpdate {
	prev := c.Result()
	final := isSSEDone(data)

	if len(data) > 0 && !final {
		c.textCollector.Consume(data)
		c.extractRefs(data)
		if raw, ok := parseJSONMap(data); ok {
			if detectMessageFinal(raw) {
				final = true
			}
			c.consumeMetadata(raw)
		}
	}

	cur := c.Result()
	return StreamMessageUpdate{
		AssistantDelta: appendedDelta(prev.AssistantText, cur.AssistantText),
		ReasoningDelta: appendedDelta(prev.ReasoningText, cur.ReasoningText),
		AssistantText:  cur.AssistantText,
		ReasoningText:  cur.ReasoningText,
		ConversationID: cur.ConversationID,
		FileIDs:        append([]string(nil), cur.FileIDs...),
		SedimentIDs:    append([]string(nil), cur.SedimentIDs...),
		FinishType:     cur.FinishType,
		ImageGenTaskID: cur.ImageGenTaskID,
		AsyncAccepted:  cur.AsyncAccepted,
		Final:          final,
	}
}

func (c *StreamMessageCollector) Result() StreamMessageUpdate {
	text := c.textCollector.Result()
	return StreamMessageUpdate{
		AssistantText:  sanitizeImageSSEText(text.AssistantText),
		ReasoningText:  sanitizeImageSSEText(text.ReasoningText),
		ConversationID: c.conversationID,
		FileIDs:        append([]string(nil), c.fileIDs...),
		SedimentIDs:    append([]string(nil), c.sedimentIDs...),
		FinishType:     c.finishType,
		ImageGenTaskID: c.imageGenTaskID,
		AsyncAccepted:  c.asyncAccepted,
	}
}

func (c *StreamMessageCollector) extractRefs(data []byte) {
	for _, m := range reFileRef.FindAllSubmatch(data, -1) {
		fid := string(m[1])
		if _, ok := c.seenFileIDs[fid]; ok {
			continue
		}
		c.seenFileIDs[fid] = struct{}{}
		c.fileIDs = append(c.fileIDs, fid)
	}
	for _, m := range reSedRef.FindAllSubmatch(data, -1) {
		sid := string(m[1])
		if _, ok := c.seenSediment[sid]; ok {
			continue
		}
		c.seenSediment[sid] = struct{}{}
		c.sedimentIDs = append(c.sedimentIDs, sid)
	}
}

func (c *StreamMessageCollector) consumeMetadata(raw map[string]interface{}) {
	if v, ok := raw["v"].(map[string]interface{}); ok {
		c.consumeValueMetadata(v)
	}
	if msg, ok := raw["message"].(map[string]interface{}); ok {
		c.consumeMessageMetadata(msg)
	}
}

func (c *StreamMessageCollector) consumeValueMetadata(v map[string]interface{}) {
	if cid, ok := v["conversation_id"].(string); ok && cid != "" && c.conversationID == "" {
		c.conversationID = cid
	}
	if msg, ok := v["message"].(map[string]interface{}); ok {
		c.consumeMessageMetadata(msg)
	}
}

func (c *StreamMessageCollector) consumeMessageMetadata(msg map[string]interface{}) {
	c.consumeAsyncSignals(msg)
	meta, _ := msg["metadata"].(map[string]interface{})
	if meta == nil {
		return
	}
	if tid, ok := meta["image_gen_task_id"].(string); ok && tid != "" {
		c.imageGenTaskID = tid
	}
	if fd, ok := meta["finish_details"].(map[string]interface{}); ok {
		if ft, ok := fd["type"].(string); ok && ft != "" {
			c.finishType = ft
		}
	}
}

func (c *StreamMessageCollector) consumeAsyncSignals(msg map[string]interface{}) {
	if c.asyncAccepted {
		return
	}
	if isAsyncPlaceholderToolMessage(msg) {
		c.asyncAccepted = true
		return
	}
	meta, _ := msg["metadata"].(map[string]interface{})
	if meta == nil {
		return
	}
	if _, ok := meta["conversation_async_status"]; ok {
		c.asyncAccepted = true
		return
	}
	sdk, _ := meta["chatgpt_sdk"].(map[string]interface{})
	if sdk == nil {
		return
	}
	toolMeta, _ := sdk["tool_response_metadata"].(map[string]interface{})
	if toolMeta == nil {
		return
	}
	if _, ok := toolMeta["conversation_async_status"]; ok {
		c.asyncAccepted = true
	}
}

func isAsyncPlaceholderToolMessage(msg map[string]interface{}) bool {
	author, _ := msg["author"].(map[string]interface{})
	if author == nil {
		return false
	}
	if role, _ := author["role"].(string); role != "tool" {
		return false
	}
	content, _ := msg["content"].(map[string]interface{})
	if content == nil {
		return false
	}
	if contentType, _ := content["content_type"].(string); contentType != "multimodal_text" {
		return false
	}
	if !isEmptyMessageParts(content["parts"]) {
		return false
	}
	status, _ := msg["status"].(string)
	return strings.TrimSpace(status) == "finished_successfully"
}

func isEmptyMessageParts(v interface{}) bool {
	switch parts := v.(type) {
	case []interface{}:
		return len(parts) == 0
	case []string:
		return len(parts) == 0
	default:
		return false
	}
}

func parseJSONMap(data []byte) (map[string]interface{}, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false
	}
	return raw, true
}

func appendedDelta(prev, cur string) string {
	if cur == "" || cur == prev {
		return ""
	}
	if prev == "" {
		return cur
	}
	if strings.HasPrefix(cur, prev) {
		return cur[len(prev):]
	}
	return cur
}

func detectMessageFinal(raw map[string]interface{}) bool {
	if t, _ := raw["type"].(string); t == "message_stream_complete" {
		return true
	}
	if v, ok := raw["v"].(string); ok && strings.TrimSpace(v) == "finished_successfully" {
		if p, _ := raw["p"].(string); p == "/message/status" {
			return true
		}
	}
	if msg, ok := raw["message"].(map[string]interface{}); ok {
		if status, _ := msg["status"].(string); status == "finished_successfully" {
			return true
		}
	}
	if arr, ok := raw["v"].([]interface{}); ok {
		for _, item := range arr {
			patch, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if p, _ := patch["p"].(string); p != "/message/status" {
				continue
			}
			if v, _ := patch["v"].(string); strings.TrimSpace(v) == "finished_successfully" {
				return true
			}
		}
	}
	return false
}

func isSSEDone(data []byte) bool {
	return strings.TrimSpace(string(data)) == "[DONE]"
}
