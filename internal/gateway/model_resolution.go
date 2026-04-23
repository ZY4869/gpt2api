package gateway

import (
	"fmt"
	"strings"

	modelpkg "github.com/432539/gpt2api/internal/model"
)

func resolveThinkingModel(chatModel *modelpkg.Model) modelpkg.ThinkingResolution {
	return modelpkg.ResolveThinkingModel(chatModel)
}

func isThinkingModel(chatModel *modelpkg.Model) bool {
	return resolveThinkingModel(chatModel).IsThinking
}

func resolveRequestedUpstreamModel(chatModel *modelpkg.Model) string {
	spec := resolveThinkingModel(chatModel)
	if spec.IsThinking {
		return mapUpstreamModelSlug(spec.ResolvedUpstreamSlug)
	}

	upstreamModel := strings.TrimSpace(chatModel.UpstreamModelSlug)
	if upstreamModel == "" {
		upstreamModel = "auto"
	}
	return mapUpstreamModelSlug(upstreamModel)
}

func modelNotFoundMessage(requestedModel string) string {
	if !modelpkg.IsThinkingSlug(requestedModel) {
		return fmt.Sprintf("模型 %q 不存在或已下架", requestedModel)
	}
	return fmt.Sprintf(
		"模型 %q 不存在或已下架。请先在模型目录启用 thinking 模型,例如 %s",
		requestedModel,
		strings.Join(modelpkg.SupportedThinkingSlugs(), " / "),
	)
}
