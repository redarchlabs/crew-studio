import type { RunEvent } from '../../../api/types'
import { num, obj } from './payload'

/** What a run has spent so far, by source, tallied from its events (live, or replayed after it ends). */
export type SpendTally = {
  llmCalls: number
  llmUsd: number
  promptTokens: number
  completionTokens: number
  cachedTokens: number
  /** Prompt tokens written to the provider's prompt cache (priced at the cache-write rate) */
  cacheWriteTokens: number
  searches: number
  searchUsd: number
  searchTokens: number
  videos: number
  videoUsd: number
}

export const emptyTally: SpendTally = {
  llmCalls: 0,
  llmUsd: 0,
  promptTokens: 0,
  completionTokens: 0,
  cachedTokens: 0,
  cacheWriteTokens: 0,
  searches: 0,
  searchUsd: 0,
  searchTokens: 0,
  videos: 0,
  videoUsd: 0,
}

/** Returns a new tally with the event's spend added; events without spend return the tally unchanged. */
export function tallyEvent(tally: SpendTally, event: RunEvent): SpendTally {
  const p = event.payload
  const cost = num(p, 'cost_usd') ?? 0
  switch (event.type) {
    case 'llm_call': {
      const usage = obj(p, 'usage') ?? {}
      return {
        ...tally,
        llmCalls: tally.llmCalls + 1,
        llmUsd: tally.llmUsd + cost,
        promptTokens: tally.promptTokens + (num(usage, 'prompt_tokens') ?? 0),
        completionTokens: tally.completionTokens + (num(usage, 'completion_tokens') ?? 0),
        cachedTokens: tally.cachedTokens + (num(usage, 'cached_prompt_tokens') ?? 0),
        cacheWriteTokens: tally.cacheWriteTokens + (num(usage, 'cache_write_tokens') ?? 0),
      }
    }
    case 'search':
      return {
        ...tally,
        searches: tally.searches + 1,
        searchUsd: tally.searchUsd + cost,
        searchTokens: tally.searchTokens + (num(p, 'input_tokens') ?? 0) + (num(p, 'output_tokens') ?? 0),
      }
    case 'video':
      return { ...tally, videos: tally.videos + 1, videoUsd: tally.videoUsd + cost }
    default:
      return tally
  }
}

export function tallyTotal(t: SpendTally): number {
  return t.llmUsd + t.searchUsd + t.videoUsd
}

/** Folds a tool name to lower snake case, so CrewAI's "google_search" and the runner's "Google Search" match. */
export function toolKey(name: string): string {
  return name
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)
    .join('_')
}
