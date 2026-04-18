package billing

import (
	modelpkg "github.com/432539/gpt2api/internal/model"
)

// ComputeChatCost 计算聊天模型的费用(单位:厘)。
// input/output tokens × 单价(per 1M) × 倍率。
func ComputeChatCost(m *modelpkg.Model, promptTokens, completionTokens int, ratio float64) int64 {
	if m == nil {
		return 0
	}
	if ratio <= 0 {
		ratio = 1.0
	}
	in := int64(promptTokens) * m.InputPricePer1M / 1_000_000
	out := int64(completionTokens) * m.OutputPricePer1M / 1_000_000
	total := float64(in+out) * ratio
	return int64(total + 0.5)
}

// ComputeImageCost 单张图费用(单位:厘)。
func ComputeImageCost(m *modelpkg.Model, n int, ratio float64) int64 {
	if m == nil {
		return 0
	}
	if ratio <= 0 {
		ratio = 1.0
	}
	if n <= 0 {
		n = 1
	}
	return int64(float64(m.ImagePricePerCall*int64(n)) * ratio)
}

// EstimateChat 预扣估算:max_tokens 未知时按保守上限 2048 估算。
func EstimateChat(m *modelpkg.Model, promptTokens, maxTokens int, ratio float64) int64 {
	out := maxTokens
	if out <= 0 {
		out = 2048
	}
	return ComputeChatCost(m, promptTokens, out, ratio)
}
