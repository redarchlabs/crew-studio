import path from 'node:path'
import { AUTH_DIR } from '../harness/paths.ts'

/** Throw-away credentials for the disposable e2e database. */
export const ADMIN = { name: 'E2E Admin', email: 'admin@e2e.test', password: 'Admin-Passw0rd-e2e!' }

export type RoleKey = 'admin' | 'builder' | 'operator' | 'viewer' | 'outsider'

export const ROLE_USERS: Record<Exclude<RoleKey, 'admin'>, { name: string; email: string; role: string; areas: string[] }> = {
  builder: { name: 'Fin Builder', email: 'builder@e2e.test', role: 'Builder', areas: ['Finance'] },
  operator: { name: 'Ops Operator', email: 'operator@e2e.test', role: 'Operator', areas: ['Finance', 'IT'] },
  viewer: { name: 'Vera Viewer', email: 'viewer@e2e.test', role: 'Viewer', areas: ['Finance', 'IT'] },
  outsider: { name: 'Hannah HR', email: 'outsider@e2e.test', role: 'Builder', areas: ['HR'] },
}

export const statePath = (role: RoleKey): string => path.join(AUTH_DIR, `${role}.json`)

/** Seeded by migration 0004/0002; ids are stable. */
export const SEED = {
  peCrew: 'PE Due Diligence Crew',
  aiCrew: 'AI Research Crew',
  peCrewId: '44444444-0000-4000-8000-000000000001',
  aiCrewId: '44444444-0000-4000-8000-000000000002',
  aiTaskId: '33333333-0000-4000-8000-000000000006',
  peTaskId: '33333333-0000-4000-8000-000000000001',
  aiAgentId: '22222222-0000-4000-8000-000000000006',
  administratorRoleId: '66666666-0000-4000-8000-000000000001',
}

/** Library items created by the 05-library spec and reused by the run, work order and trigger specs. */
export const LIB = {
  area: 'Training',
  visionLlm: 'E2E Vision LLM',
  plainLlm: 'E2E Plain LLM',
  model: 'openai/gpt-6-luna', // priced by the seeded pricing table, so runs can be costed
  agent: 'E2E Analyst',
  agentRole: 'Analyst for {topic}',
  task1: 'E2E Gather',
  task2: 'E2E Summarize',
  crew: 'E2E Crew',
  topicDefault: 'quantum computing',
}
