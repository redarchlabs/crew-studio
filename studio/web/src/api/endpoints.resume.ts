import { request } from './client'
import type { Run } from './types'

export type ResumeRunBody = {
  /** The new run's budget for its own spending; left out uses the earlier run's */
  budget_usd?: number
  /** Run the remaining tasks with the crew's current settings instead of the ones the run started with */
  use_current?: boolean
}

export const resumeApi = {
  /** Starts a run that reuses a failed or cancelled run's finished tasks and runs the rest */
  resume: (runId: string, body: ResumeRunBody = {}) => request<Run>('POST', `/runs/${runId}/resume`, { body }),
}
