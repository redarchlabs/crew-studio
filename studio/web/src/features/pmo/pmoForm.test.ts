import { describe, expect, it } from 'vitest'
import type { PMOOptions } from '../../api/types.delivery'
import type { KM2FolderList } from '../../api/types.crewstate'
import { emptyForm, inAreas, km2DestinationProblems, nameProblem, rowErrors, suggestedName, toRequest, validateStep } from './pmoForm'

const opts: PMOOptions = {
  trackers: [{ id: 't1', name: 'Acme Jira', kind: 'jira', area_ids: ['a1'] }], destinations: [], llms: [],
  coordinator_llm_id: 'l-cheap', reporter_llm_id: null, sweep_cron: '0 7 * * 1-5', report_cron: '0 8 * * 1',
}

describe('pmoForm', () => {
  it('starts from the options and builds the request', () => {
    const f = emptyForm(['a1'], 'UTC', opts)
    expect(f.tracker_connection_id).toBe('t1')
    expect(f.coordinator_llm_id).toBe('l-cheap')
    expect(f.reporter_llm_id).toBe('')
    expect(emptyForm([], 'UTC', { ...opts, trackers: [...opts.trackers, { id: 't2', name: 'Linear' }] }).tracker_connection_id).toBe('')
    expect(toRequest({ ...f, name: ' Atlas ', destinations: [{ kind: 'email', connection_id: 'm', recipients: 'a@x.io', folder: '' }] })).toEqual({
      name: 'Atlas', area_ids: ['a1'], timezone: 'UTC', tracker_connection_id: 't1', coordinator_llm_id: 'l-cheap', reporter_llm_id: null,
      destinations: [{ kind: 'email', connection_id: 'm', recipients: ['a@x.io'], folder: '' }], enabled: true,
    })
  })

  it('checks each step', () => {
    const f = emptyForm([], '', { ...opts, trackers: [] })
    expect(validateStep(0, { ...f, name: '' })).toEqual({ name: 'Give the PMO a name', area_ids: 'Pick at least one functional area', timezone: 'Choose a time zone' })
    expect(validateStep(0, { ...f, name: 'x'.repeat(81), area_ids: ['a'], timezone: 'UTC' })).toEqual({ name: 'At most 80 characters' })
    expect(validateStep(1, f)).toEqual({ tracker_connection_id: 'Choose the tracker connection to follow' })
    const errs = validateStep(2, { ...f, destinations: [{ kind: 'slack', connection_id: '', recipients: '', folder: '' }] })
    expect(rowErrors({ ...errs, name: 'x' })).toEqual({ 0: 'Choose a connection' })
    expect(validateStep(3, { ...f, coordinator_llm_id: '', reporter_llm_id: '' })).toEqual({ coordinator_llm_id: 'Choose an LLM', reporter_llm_id: 'Choose an LLM' })
  })

  it('offers connections in the chosen areas', () => {
    const choices = [{ id: '1', name: 'A', area_ids: ['a1'] }, { id: '2', name: 'B', area_ids: ['a2'] }, { id: '3', name: 'C' }]
    expect(inAreas(choices, []).map((c) => c.id)).toEqual(['1', '2', '3'])
    expect(inAreas(choices, ['a1']).map((c) => c.id)).toEqual(['1', '3'])
  })

  it('suggests a name that does not take the template agent name', () => {
    expect(emptyForm([], 'UTC', opts).name).toBe('Acme Jira PMO')
    expect(emptyForm([], 'UTC', { ...opts, trackers: [] }).name).toBe('Team PMO')
    expect(suggestedName(undefined)).toBe('Team PMO')
    expect(nameProblem(['area_ids: x', 'name: an agent named "PMO Coordinator" already exists; choose another name'])).toBe(
      'an agent named "PMO Coordinator" already exists; choose another name')
    expect(nameProblem(['area_ids: x'])).toBeUndefined()
  })

  it('finds KM2 destinations that could not file anything', async () => {
    const list = (def: string, ok = true) => ({ ok, message: '', folders: [], folder_count: 0, warnings: [], allowed: { default_folder_id: def, write_folder_ids: [] } }) as KM2FolderList
    const lists: Record<string, KM2FolderList> = { none: list(''), some: list('f1'), broken: list('', false) }
    const load = async (id: string) => {
      if (id === 'offline') throw new Error('offline')
      return lists[id]
    }
    const rows = [
      { kind: 'km2' as const, connection_id: 'none', recipients: '', folder: '' },
      { kind: 'km2' as const, connection_id: 'none', recipients: '', folder: 'f2' },
      { kind: 'km2' as const, connection_id: 'some', recipients: '', folder: '' },
      { kind: 'km2' as const, connection_id: 'broken', recipients: '', folder: '' },
      { kind: 'km2' as const, connection_id: 'offline', recipients: '', folder: '' },
      { kind: 'slack' as const, connection_id: 'none', recipients: '', folder: '' },
      { kind: 'km2' as const, connection_id: '', recipients: '', folder: '' },
    ]
    expect(await km2DestinationProblems(rows, { none: 'KM2 Wiki' }, load)).toEqual({
      'destinations.0': 'KM2 Wiki has no default folder, so reports cannot be filed: pick a folder, or set a default folder on the connection.',
    })
    expect(await km2DestinationProblems([rows[0]], {}, load)).toEqual({ 'destinations.0': expect.stringMatching(/^This connection has no default folder/) })
  })
})
