import fs from 'node:fs'
import { STATE_FILE } from './paths.ts'
import { killGroup } from './proc.ts'
import type { HarnessState } from './state.ts'

export default async function globalTeardown(): Promise<void> {
  if (!fs.existsSync(STATE_FILE)) return
  const state = JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')) as HarnessState
  await Promise.all([killGroup(state.pids.server), killGroup(state.pids.fakeLlm), ...(state.pids.fakeTracker ? [killGroup(state.pids.fakeTracker)] : []),
    ...(state.pids.fakeKm2 ? [killGroup(state.pids.fakeKm2)] : []), ...(state.pids.fakeDelivery ? [killGroup(state.pids.fakeDelivery)] : []),
    ...(state.pids.fakeMcp ? [killGroup(state.pids.fakeMcp)] : [])])
  console.log('[e2e] stopped the Crew Studio server and the fake LLM server')
}
