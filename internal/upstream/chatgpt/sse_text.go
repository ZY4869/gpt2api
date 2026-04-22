package chatgpt

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

type sseTextResult struct {
	AssistantText string
	ReasoningText string
}

type sseTextCollector struct {
	curP          string
	recipient     string
	assistant     strings.Builder
	thoughtParts  map[string]*strings.Builder
	lastAssistant string
}

func newSSETextCollector() *sseTextCollector {
	return &sseTextCollector{
		recipient:    "all",
		thoughtParts: map[string]*strings.Builder{},
	}
}

func (c *sseTextCollector) Consume(data []byte) {
	if len(data) == 0 || strings.TrimSpace(string(data)) == "[DONE]" {
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	if p, ok := raw["p"].(string); ok {
		c.curP = p
	}
	if v, ok := raw["v"]; ok {
		c.consumeValue(c.curP, v)
		return
	}
	if msg, ok := raw["message"].(map[string]interface{}); ok {
		c.consumeMessage(msg)
	}
}

func (c *sseTextCollector) Result() sseTextResult {
	return sseTextResult{
		AssistantText: strings.TrimSpace(c.assistant.String()),
		ReasoningText: strings.TrimSpace(c.joinThoughtParts()),
	}
}

func (c *sseTextCollector) consumeValue(path string, v interface{}) {
	switch x := v.(type) {
	case string:
		c.consumeString(path, x, "")
	case []interface{}:
		for _, item := range x {
			patch, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			subPath := path
			if p, ok := patch["p"].(string); ok && p != "" {
				subPath = p
				c.curP = p
			}
			if subPath == "/message/recipient" {
				if s, ok := patch["v"].(string); ok && s != "" {
					c.recipient = s
				}
				continue
			}
			op, _ := patch["o"].(string)
			c.consumeValueWithOp(subPath, patch["v"], op)
		}
	case map[string]interface{}:
		if msg, ok := x["message"].(map[string]interface{}); ok {
			c.consumeMessage(msg)
		}
	}
}

func (c *sseTextCollector) consumeValueWithOp(path string, v interface{}, op string) {
	switch x := v.(type) {
	case string:
		c.consumeString(path, x, op)
	case map[string]interface{}:
		if msg, ok := x["message"].(map[string]interface{}); ok {
			c.consumeMessage(msg)
		}
	case []interface{}:
		for _, item := range x {
			c.consumeValueWithOp(path, item, op)
		}
	}
}

func (c *sseTextCollector) consumeString(path, text, op string) {
	if text == "" {
		return
	}
	if path == "/message/recipient" {
		c.recipient = text
		return
	}
	if path == "/message/status" {
		return
	}
	if strings.HasPrefix(path, "/message/content/thoughts") {
		c.appendThought(path, text, op)
		return
	}
	if c.recipient != "all" {
		return
	}
	if path == "" || path == "/message/content/parts/0" {
		if op == "replace" {
			c.replaceAssistant(text)
			return
		}
		c.assistant.WriteString(text)
	}
}

func (c *sseTextCollector) consumeMessage(msg map[string]interface{}) {
	if recipient, ok := msg["recipient"].(string); ok && recipient != "" {
		c.recipient = recipient
	}
	content, _ := msg["content"].(map[string]interface{})
	if content == nil {
		return
	}
	if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
		if first, ok := parts[0].(string); ok && first != "" {
			c.replaceAssistant(first)
		}
	}
	if thoughts, ok := content["thoughts"]; ok {
		c.consumeThoughtValue("/message/content/thoughts", thoughts)
	}
}

func (c *sseTextCollector) consumeThoughtValue(path string, v interface{}) {
	switch x := v.(type) {
	case string:
		c.appendThought(path, x, "replace")
	case []interface{}:
		for i, item := range x {
			c.consumeThoughtValue(path+"/"+itoa(i), item)
		}
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return thoughtPathRank(keys[i]) < thoughtPathRank(keys[j]) ||
				(thoughtPathRank(keys[i]) == thoughtPathRank(keys[j]) && keys[i] < keys[j])
		})
		for _, k := range keys {
			c.consumeThoughtValue(path+"/"+k, x[k])
		}
	}
}

func (c *sseTextCollector) appendThought(path, text, op string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	b := c.thoughtParts[path]
	if b == nil {
		b = &strings.Builder{}
		c.thoughtParts[path] = b
	}
	if op == "replace" {
		*b = strings.Builder{}
	}
	b.WriteString(text)
}

func (c *sseTextCollector) replaceAssistant(text string) {
	text = strings.TrimSpace(text)
	if text == "" || text == c.lastAssistant {
		return
	}
	c.lastAssistant = text
	c.assistant = strings.Builder{}
	c.assistant.WriteString(text)
}

func (c *sseTextCollector) joinThoughtParts() string {
	if len(c.thoughtParts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(c.thoughtParts))
	for k := range c.thoughtParts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return thoughtPathRank(keys[i]) < thoughtPathRank(keys[j]) ||
			(thoughtPathRank(keys[i]) == thoughtPathRank(keys[j]) && keys[i] < keys[j])
	})

	parts := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	for _, k := range keys {
		text := strings.TrimSpace(c.thoughtParts[k].String())
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func thoughtPathRank(path string) int {
	switch {
	case strings.Contains(path, "/summary"):
		return 0
	case strings.Contains(path, "/content"):
		return 1
	default:
		return 2
	}
}
