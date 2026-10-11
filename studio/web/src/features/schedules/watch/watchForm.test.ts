import { describe, expect, it } from 'vitest'
import { makeWatch } from './testUtils'
import { defaultLadder, emptyStep, emptyWatchForm, hasItems, splitList, toWatchForm, toWatchInput, validateWatchForm, type WatchForm } from './watchForm'

const form = (over: Partial<WatchForm> = {}): WatchForm => ({ ...emptyWatchForm(), ...over })

describe('watch form', () => {
  it('splits lists', () => {
    expect(splitList(' a, b\nc,, a ')).toEqual(['a', 'b', 'c'])
    expect(splitList('')).toEqual([])
  })

  it('builds an http_json request with only its fields', () => {
    const input = toWatchInput(form({ connection_id: 'k1', path: ' /issues ', items_path: 'data.items', url: 'ignored', id_field: ' key ', tracked_fields: 'status, due', max_items: 50 }))
    expect(input).toEqual({
      connection_id: 'k1', source: { kind: 'http_json', path: '/issues', items_path: 'data.items' }, id_field: 'key',
      tracked_fields: ['status', 'due'], run_when: 'changed', followups: null, dry_run: true, max_items: 50,
    })
  })

  it('builds web page and workspace file requests', () => {
    const page = toWatchInput(form({ kind: 'web_page', url: ' https://x/status ', selector: 'main', id_field: 'x', tracked_fields: 'y', ladder_on: true, connection_id: 'k1' }))
    expect(page.source).toEqual({ kind: 'web_page', url: 'https://x/status', selector: 'main' })
    expect(page.connection_id).toBeNull()
    expect(page.id_field).toBe('')
    expect(page.tracked_fields).toEqual([])
    expect(page.followups).toBeNull()
    expect(page.max_items).toBe(0)
    const csv = toWatchInput(form({ kind: 'workspace_file', file: 'inbox/a.csv', format: 'csv', items_path: 'x' }))
    expect(csv.source).toEqual({ kind: 'workspace_file', file: 'inbox/a.csv', format: 'csv' })
    const json = toWatchInput(form({ kind: 'workspace_file', file: 'a.json', format: '', items_path: 'items' }))
    expect(json.source).toEqual({ kind: 'workspace_file', file: 'a.json', format: '', items_path: 'items' })
    expect(toWatchInput(form({ kind: 'workspace_file', file: 'A.CSV', format: '', items_path: 'x' })).source).toEqual({ kind: 'workspace_file', file: 'A.CSV', format: '' })
    expect(toWatchInput(form({ kind: 'workspace_file', file: 'a.csv', format: 'json', items_path: 'x' })).source.items_path).toBe('x')
  })

  it('builds a ladder', () => {
    const ladder = { ...defaultLadder(), date_field: 'due', skip: [{ field: 'status', values: 'Done, Canceled' }, { field: '', values: 'x' }], max_per_tick: 10, timezone: ' UTC ' }
    const input = toWatchInput(form({ ladder_on: true, ladder, tracked_fields: 'due, status' }))
    expect(input.followups).toEqual({
      date_field: 'due', skip_when: { status: ['Done', 'Canceled'] }, quiet_days: ['Sat', 'Sun'], timezone: 'UTC', max_per_tick: 10,
      steps: [
        { name: 'due_soon', offset: '-3d', notify: 'owner' },
        { name: 'due_today', offset: '0d', notify: 'owner' },
        { name: 'overdue', offset: '+2d', notify: 'lead', repeat: '7d' },
      ],
    })
    expect(toWatchInput(form({ ladder_on: true, ladder: { ...ladder, max_per_tick: null } })).followups?.max_per_tick).toBeUndefined()
  })

  it('round-trips a saved watch', () => {
    const w = makeWatch()
    const f = toWatchForm(w)
    expect(f.tracked_fields).toBe('status, due')
    expect(f.ladder_on).toBe(true)
    expect(f.ladder.skip).toEqual([{ field: 'status', values: 'Done' }])
    expect(toWatchInput(f)).toEqual({
      connection_id: w.connection_id, source: w.source, id_field: w.id_field, tracked_fields: w.tracked_fields, run_when: w.run_when,
      followups: { ...w.followups, timezone: '' }, dry_run: w.dry_run, max_items: w.max_items,
    })
    const bare = toWatchForm(makeWatch({ followups: null, connection_id: null, source: { kind: 'web_page', url: 'https://x' } }))
    expect(bare).toMatchObject({ ladder_on: false, connection_id: '', url: 'https://x', path: '', id_field: '', tracked_fields: '' })
    const noSkip = toWatchForm(makeWatch({ followups: { date_field: 'due', steps: [{ name: 'a', offset: '0d', notify: 'o' }] } }))
    expect(noSkip.ladder).toMatchObject({ skip: [{ field: '', values: '' }], quiet_days: [], timezone: '', max_per_tick: null })
    expect(noSkip.ladder.steps[0].repeat).toBe('')
  })

  it('validates each kind', () => {
    expect(validateWatchForm(form())).toMatchObject({ connection_id: expect.any(String), path: expect.any(String), id_field: expect.any(String), tracked_fields: expect.any(String) })
    expect(validateWatchForm(form({ connection_id: 'k', path: '//x', id_field: 'k', tracked_fields: 'a' }))).toHaveProperty('path')
    expect(validateWatchForm(form({ connection_id: 'k', path: '/x', id_field: 'k', tracked_fields: 'a' }))).toEqual({})
    expect(validateWatchForm(form({ kind: 'web_page', url: 'ftp://x' }))).toEqual({ url: expect.any(String) })
    expect(validateWatchForm(form({ kind: 'web_page', url: 'https://x/y' }))).toEqual({})
    expect(validateWatchForm(form({ kind: 'workspace_file', id_field: 'id', tracked_fields: 'a' }))).toEqual({ file: expect.any(String) })
    expect(validateWatchForm(form({ kind: 'workspace_file', file: 'inbox/a.csv', id_field: 'id', tracked_fields: 'a' }))).toEqual({ file: expect.any(String) })
    expect(validateWatchForm(form({ kind: 'workspace_file', file: 'watch/a.csv', id_field: 'id', tracked_fields: 'a' }))).toEqual({})
    expect(validateWatchForm(form({ kind: 'web_page', url: 'https://x', max_items: 0 }))).toEqual({ max_items: expect.any(String) })
    expect(validateWatchForm(form({ kind: 'web_page', url: 'https://x', max_items: Number.NaN }))).toHaveProperty('max_items')
    expect(hasItems('web_page')).toBe(false)
  })

  it('validates the ladder', () => {
    const base = form({ kind: 'workspace_file', file: 'watch/a.csv', id_field: 'id', tracked_fields: 'due, status', ladder_on: true })
    const check = (ladder: Partial<WatchForm['ladder']>) => validateWatchForm({ ...base, ladder: { ...defaultLadder(), date_field: 'due', ...ladder } })
    expect(check({})).toEqual({})
    expect(validateWatchForm({ ...base, ladder: defaultLadder() })).toHaveProperty('ladder:date_field', 'Pick the date field each step counts from.')
    expect(check({ date_field: 'other' })).toHaveProperty('ladder:date_field', 'The date field must be one of the tracked fields.')
    expect(check({ steps: [] })).toHaveProperty('ladder:steps')
    expect(check({ steps: [{ ...emptyStep(), name: 'Bad Name', offset: '0d' }] })['ladder:step:0']).toMatch(/Name/)
    expect(check({ steps: [{ name: 'a', offset: '0d', notify: 'o', repeat: '' }, { name: 'a', offset: '1d', notify: 'o', repeat: '' }] })['ladder:step:1']).toMatch(/twice/)
    expect(check({ steps: [{ name: 'a', offset: '3 days', notify: 'o', repeat: '' }] })['ladder:step:0']).toMatch(/Offset/)
    expect(check({ steps: [{ name: 'a', offset: '0d', notify: ' ', repeat: '' }] })['ladder:step:0']).toMatch(/notify/)
    expect(check({ steps: [{ name: 'a', offset: '0d', notify: 'o', repeat: '0d' }] })['ladder:step:0']).toMatch(/Repeat/)
    expect(check({ quiet_days: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] })).toHaveProperty('ladder:quiet_days')
    expect(check({ skip: [{ field: 'owner', values: 'x' }] })).toHaveProperty('ladder:skip', 'Skip rules use tracked fields; owner is not one.')
    expect(check({ skip: [{ field: 'status', values: 'Done' }, { field: '', values: '' }] })).toEqual({})
  })
})
