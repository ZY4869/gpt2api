import type { SimpleModel } from '@/api/me'

export const STABLE_THINKING_MODEL = 'gpt-5-thinking'

function chatModelRank(model: SimpleModel): number {
  if (model.slug === STABLE_THINKING_MODEL) return 0
  if (model.is_thinking) return 1
  return 2
}

export function sortChatModelsForDisplay(models: SimpleModel[]): SimpleModel[] {
  return [...models].sort((a, b) => {
    const rankDiff = chatModelRank(a) - chatModelRank(b)
    if (rankDiff !== 0) return rankDiff
    return a.slug.localeCompare(b.slug)
  })
}

export function preferredThinkingModelSlug(models: SimpleModel[]): string {
  const stable = models.find((model) => model.slug === STABLE_THINKING_MODEL)
  if (stable) return stable.slug
  return models.find((model) => model.is_thinking)?.slug || ''
}

export function pickDefaultChatModel(models: SimpleModel[]): string {
  return preferredThinkingModelSlug(models) || models[0]?.slug || ''
}
