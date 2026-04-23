package model

type PublicModel struct {
	ID          uint64 `json:"id"`
	Slug        string `json:"slug"`
	Type        string `json:"type"`
	Description string `json:"description"`
	IsThinking  bool   `json:"is_thinking"`
}

func BuildPublicModels(rows []*Model) []PublicModel {
	out := make([]PublicModel, 0, len(rows))
	for _, m := range rows {
		if m == nil {
			continue
		}
		out = append(out, PublicModel{
			ID:          m.ID,
			Slug:        m.Slug,
			Type:        m.Type,
			Description: m.Description,
			IsThinking:  ResolveThinkingModel(m).IsThinking,
		})
	}
	return out
}
