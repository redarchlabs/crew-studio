import type { AgentChatSettingsInput } from '../../api/types.chat'
import type { EscalationOnTimeout } from '../../api/types.human'
import { NumberField, SelectField, TextAreaField, Toggle } from '../../components/Field'

/** Question limits when the server sends none (model.DefaultQuestionTimeoutMinutes, DefaultChatMaxQuestions) and their ranges */
export const QUESTION_DEFAULTS = { timeout: 30, max: 3 }
export const MAX_QUESTION_TIMEOUT = 180
export const MAX_QUESTIONS = 20

const RULES_PLACEHOLDER = `- Ask before choosing between price models (list or negotiated).
- Ask before assuming a region, budget or deadline the brief does not give.`

const ON_TIMEOUT: { value: EscalationOnTimeout; label: string }[] = [
  { value: 'proceed', label: 'Carry on and state the assumption' },
  { value: 'fail', label: 'Stop the hand-off' },
]

type Props = {
  v: AgentChatSettingsInput
  set: <K extends keyof AgentChatSettingsInput>(key: K, value: AgentChatSettingsInput[K]) => void
  errors: Record<string, string>
  canEdit: boolean
}

/**
 * Questions during a hand-off (docs/agent-chat-handoffs-design.md, section 6): a specialist may always ask the lead; with
 * these on, the lead may bring a question to the person as a Waiting for you card in the conversation. Mirrors a crew's
 * Human input panel.
 */
export function QuestionFields({ v, set, errors, canEdit }: Props) {
  return (
    <section aria-label="Questions" className="flex flex-col gap-3 border-t border-line pt-3">
      <Toggle label="The lead may ask the person during a hand-off" checked={v.questions_enabled} onChange={(x) => set('questions_enabled', x)}
        disabled={!canEdit}
        hint="Specialists can always ask the lead. With this on, when only the person can answer, the lead asks them in the conversation (a Waiting for you card, and a badge on Chat) and the hand-off waits." />
      {v.questions_enabled && (
        <>
          <TextAreaField label="When to ask the person" value={v.question_rules} onChange={(x) => set('question_rules', x)} rows={4}
            placeholder={RULES_PLACEHOLDER} error={errors.question_rules}
            hint="Plain language. Outside these rules the lead and its specialists decide for themselves and state their assumptions." />
          <div className="grid gap-3 sm:grid-cols-3">
            <NumberField label="Wait for an answer (minutes)" value={v.question_timeout_minutes}
              onChange={(x) => set('question_timeout_minutes', x ?? Number.NaN)} min={1} max={MAX_QUESTION_TIMEOUT} step={1}
              error={errors.question_timeout_minutes} hint="The hand-off's time limit keeps running while it waits." />
            <SelectField label="If nobody answers" value={v.question_on_timeout} onChange={(x) => set('question_on_timeout', x as EscalationOnTimeout)}
              options={ON_TIMEOUT} error={errors.question_on_timeout} />
            <NumberField label="Questions per hand-off" value={v.max_questions} onChange={(x) => set('max_questions', x ?? Number.NaN)}
              min={1} max={MAX_QUESTIONS} step={1} error={errors.max_questions} hint={`1 to ${MAX_QUESTIONS}.`} />
          </div>
        </>
      )}
    </section>
  )
}
