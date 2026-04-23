package chatgpt

import (
	"encoding/json"
	"strings"
)

type StreamMessageUpdate struct {
	AssistantDelta       string
	ReasoningDelta       string
	AssistantText        string
	ReasoningText        string
	ThinkingTriggered    bool
	ThinkingTriggeredVia string
	SawThoughtPatch      bool
	SawThinkingMetadata  bool
	ConversationID       string
	FileIDs              []string
	SedimentIDs          []string
	FinishType           string
	ImageGenTaskID       string
	Final                bool
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
		AssistantDelta:       appendedDelta(prev.AssistantText, cur.AssistantText),
		ReasoningDelta:       appendedDelta(prev.ReasoningText, cur.ReasoningText),
		AssistantText:        cur.AssistantText,
		ReasoningText:        cur.ReasoningText,
		ThinkingTriggered:    cur.ThinkingTriggered,
		ThinkingTriggeredVia: cur.ThinkingTriggeredVia,
		SawThoughtPatch:      cur.SawThoughtPatch,
		SawThinkingMetadata:  cur.SawThinkingMetadata,
		ConversationID:       cur.ConversationID,
		FileIDs:              append([]string(nil), cur.FileIDs...),
		SedimentIDs:          append([]string(nil), cur.SedimentIDs...),
		FinishType:           cur.FinishType,
		ImageGenTaskID:       cur.ImageGenTaskID,
		Final:                final,
	}
}

func (c *StreamMessageCollector) Result() StreamMessageUpdate {
	text := c.textCollector.Result()
	reasoningText := sanitizeImageSSEText(text.ReasoningText)
	return StreamMessageUpdate{
		AssistantText:        sanitizeImageSSEText(text.AssistantText),
		ReasoningText:        reasoningText,
		ThinkingTriggered:    thinkingTriggered(reasoningText, text.SawThoughtPatch, text.SawThinkingMetadata),
		ThinkingTriggeredVia: thinkingTriggeredVia(reasoningText, text.SawThoughtPatch, text.SawThinkingMetadata),
		SawThoughtPatch:      text.SawThoughtPatch,
		SawThinkingMetadata:  text.SawThinkingMetadata,
		ConversationID:       c.conversationID,
		FileIDs:              append([]string(nil), c.fileIDs...),
		SedimentIDs:          append([]string(nil), c.sedimentIDs...),
		FinishType:           c.finishType,
		ImageGenTaskID:       c.imageGenTaskID,
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
