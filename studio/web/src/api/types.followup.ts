// Work order follow-up: talking with the crew about a work order and revising it with feedback. Mirror studio/docs/api.md.

import type { Run } from './types'

export type WorkOrderFeedback = {
  id: string
  work_order_id: string
  /** The attempt the feedback is about */
  run_id: string | null
  /** The attempt started to act on it */
  revision_run_id: string | null
  feedback: string
  /** Whether the person's conversation about the work order was passed to the revision */
  with_conversation: boolean
  created_by_name: string
  created_at: string
}

export type ReviseInput = { feedback: string; include_conversation: boolean }

export type ReviseResult = { run: Run; feedback: WorkOrderFeedback }
