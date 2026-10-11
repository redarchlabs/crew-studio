import fs from 'node:fs'
import { readState } from '../harness/state.ts'

export type LlmRequest = {
  ts: number
  path: string
  model: string
  stream: boolean
  image_count: number
  image_mimes: string[]
  text: string
  has_tools: boolean
  /** Function names of the tools sent (chat completions) */
  tool_names?: string[]
}

/** Every request the fake LLM server has received so far (it appends one JSON line per request). */
export function llmRequests(): LlmRequest[] {
  try {
    return fs
      .readFileSync(readState().llmLog, 'utf8')
      .split('\n')
      .filter((l) => l.trim())
      .map((l) => JSON.parse(l) as LlmRequest)
  } catch {
    return []
  }
}

/** Requests received after the first `skip` ones, so a test can look only at what it caused. */
export function llmRequestsSince(skip: number): LlmRequest[] {
  return llmRequests().slice(skip)
}
