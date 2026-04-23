package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const ConversationStreamStatusComplete = "COMPLETE"

// GetConversationStreamStatus 查询会话异步回图状态。
func (c *Client) GetConversationStreamStatus(ctx context.Context, convID string) (string, error) {
	if convID == "" {
		return "", fmt.Errorf("conv_id required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.opts.BaseURL+"/backend-api/conversation/"+url.PathEscape(convID)+"/stream_status", nil)
	if err != nil {
		return "", err
	}
	c.commonHeaders(req)
	req.Header.Set("Accept", "*/*")

	res, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return "", &UpstreamError{Status: res.StatusCode, Message: "conversation stream_status failed", Body: string(buf)}
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(buf, &out); err != nil {
		return "", fmt.Errorf("decode stream_status: %w", err)
	}
	return out.Status, nil
}
