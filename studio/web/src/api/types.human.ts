// Human input: a crew's escalation rules and the questions its agents ask people during runs. Mirror studio/docs/api.md.

export type EscalationOnTimeout = 'proceed' | 'fail'

export type EscalationSettings = {
  crew_id: string
  enabled: boolean
  /** When to ask a person, in plain language; it goes into every agent's instructions */
  rules: string
  timeout_minutes: number
  on_timeout: EscalationOnTimeout
  max_questions: number
  updated_by_name: string
  updated_at: string | null
}

export type EscalationSettingsInput = Pick<EscalationSettings, 'enabled' | 'rules' | 'timeout_minutes' | 'on_timeout' | 'max_questions'>

export type QuestionStatus = 'open' | 'answered' | 'timed_out' | 'closed'

export type HumanQuestion = {
  id: string
  run_id: string
  work_order_id: string | null
  work_order_number: number | null
  work_order_title: string
  crew_name: string
  /** The person asked; null when a service account, trigger or schedule started the run (work order operators answer) */
  assignee_id: string | null
  agent_role: string
  /** The agent it was asked for: a hand-off's lead asks on behalf of a specialist ("asked by asked_for through agent_role") */
  asked_for?: string
  task_name: string
  question: string
  context: string
  status: QuestionStatus
  answer: string
  answered_by_name: string
  asked_at: string
  deadline_at: string
  answered_at: string | null
  /** Set for a question from an agent chat's hand-off: it is answered in that conversation (older servers leave these out) */
  conversation_id?: string | null
  conversation_title?: string
  handoff_id?: string | null
  handoff_title?: string
}
