import { describe, expect, it } from 'vitest'
import type { RunEvent, RunEventType } from '../../../api/types'
import { emptyTally, tallyEvent, toolKey } from './spendTally'

const ev = (type: RunEventType, payload: Record<string, unknown>): RunEvent => ({ seq: 1, type, payload, created_at: '2026-01-01T00:00:00Z' })

describe('tallyEvent', () => {
  it('adds LLM calls with their tokens and cost', () => {
    const t = [
      ev('llm_call', { cost_usd: 0.001, usage: { prompt_tokens: 300, completion_tokens: 20, cached_prompt_tokens: 100 } }),
      ev('llm_call', { cost_usd: 0.002, usage: { prompt_tokens: 500, completion_tokens: 30, cache_write_tokens: 400 } }),
    ].reduce(tallyEvent, emptyTally)
    expect(t).toMatchObject({ llmCalls: 2, promptTokens: 800, completionTokens: 50, cachedTokens: 100, cacheWriteTokens: 400 })
    expect(t.llmUsd).toBeCloseTo(0.003)
  })

  it('counts searches and videos separately from LLM spend', () => {
    const t = [
      ev('search', { cost_usd: 0.006, input_tokens: 40, output_tokens: 1500 }),
      ev('search', { cost_usd: 0.004, input_tokens: 30, output_tokens: 900 }),
      ev('video', { cost_usd: 0.8 }),
    ].reduce(tallyEvent, emptyTally)
    expect(t).toMatchObject({ llmCalls: 0, searches: 2, videos: 1, searchTokens: 2470 })
    expect(t.searchUsd).toBeCloseTo(0.01)
    expect(t.videoUsd).toBeCloseTo(0.8)
  })

  it('returns the same object for events that carry no spend', () => {
    expect(tallyEvent(emptyTally, ev('tool_started', { tool_name: 'x' }))).toBe(emptyTally)
  })
})

describe('toolKey', () => {
  it('folds display and sanitized tool names together', () => {
    expect(toolKey('Google Search')).toBe('google_search')
    expect(toolKey('google_search')).toBe('google_search')
    expect(toolKey(' Read  Workspace-File ')).toBe('read_workspace_file')
  })
})
