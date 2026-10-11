import fs from 'node:fs'
import { STATE_FILE } from './paths.ts'

export type HarnessState = {
  baseURL: string
  fakeLlmURL: string
  llmLog: string
  serverLog: string
  setupToken: string
  dbName: string
  pids: { server: number; fakeLlm: number; fakeTracker?: number; fakeKm2?: number; fakeDelivery?: number; fakeMcp?: number }
  runtimeDir: string
}

export function readState(): HarnessState {
  return JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')) as HarnessState
}

export function writeState(state: HarnessState): void {
  fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2))
}
