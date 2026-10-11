import { describe, expect, it } from 'vitest'
import { makeCrew } from '../../lib/testFixtures'
import type { CrewDraft } from '../../api/types'
import { crewBodyFromDraft, newCrewBody, toCrewBody, validateCrew } from './crewForm'

describe('crew form areas', () => {
  it('requires at least one area', () => {
    const v = { ...newCrewBody([]), name: 'Crew' }
    expect(validateCrew(v)).toEqual({ area_ids: expect.stringMatching(/at least one/) })
    expect(validateCrew({ ...v, area_ids: ['fin'] })).toEqual({})
  })

  it('keeps the crew areas when editing', () => {
    expect(toCrewBody(makeCrew({ area_ids: ['a', 'b'] })).area_ids).toEqual(['a', 'b'])
  })
})

describe('crewBodyFromDraft', () => {
  const draft: CrewDraft = {
    name: 'Memo (task)',
    description: 'Runs Memo',
    process: 'sequential',
    agent_ids: ['a1'],
    task_ids: ['t0', 't1'],
    inputs: [{ key: 'company', label: 'Company', default: '', required: true }],
    budget_usd: 2,
    area_ids: ['fin'],
  }

  it('uses the draft as a form value with no manager LLM', () => {
    expect(crewBodyFromDraft(draft)).toEqual({ ...draft, manager_llm_id: null })
  })

  it('copies arrays and inputs so the form never edits the response', () => {
    const body = crewBodyFromDraft(draft)
    expect(body.task_ids).not.toBe(draft.task_ids)
    expect(body.inputs[0]).not.toBe(draft.inputs[0])
  })

  it('fills fields the server leaves out or sends as null', () => {
    const sparse = { name: 'X', budget_usd: 3, agent_ids: null, task_ids: null, inputs: null, area_ids: null } as unknown as CrewDraft
    expect(crewBodyFromDraft(sparse)).toEqual({ ...newCrewBody([]), name: 'X', budget_usd: 3 })
    expect(crewBodyFromDraft({ ...draft, manager_llm_id: 'l1', budget_usd: undefined as unknown as number }).budget_usd).toBe(1)
    expect(crewBodyFromDraft({} as CrewDraft).name).toBe('')
  })
})
