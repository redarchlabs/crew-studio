import { cleanup, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { crewStateApi } from '../../../api/endpoints.crewstate'
import type { ActionEntry } from '../../../api/types.crewstate'
import { renderRoute, setupApi } from '../testUtils'
import { KM2SavesPanel, saveOutcome } from './KM2SavesPanel'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.crewstate')

const TS = '2026-10-05T07:00:00Z'
const save = (over: Partial<ActionEntry>): ActionEntry => ({
  id: 'e1', crew_id: 'c1', schedule_id: null, schedule_name: '', subject: 'km2:atlas-w41', action: 'km2:save', run_id: 'r1', dry_run: false,
  details: { title: 'Atlas weekly W41', external_ref: 'atlas-w41', connection: 'KM2 Finance', folder_id: 'f1', outcome: 'created' },
  idempotency_key: null, at: TS, ...over,
})

beforeEach(() => setupApi())
afterEach(cleanup)

describe('saveOutcome', () => {
  it.each([
    [save({ dry_run: true }), 'dry run, not sent', 'warn'],
    [save({ details: { would_save: true } }), 'dry run, not sent', 'warn'],
    [save({}), 'created', 'ok'],
    [save({ details: { outcome: 'updated' } }), 'updated', 'ok'],
    [save({ details: { outcome: 'metadata_updated' } }), 'metadata updated', 'neutral'],
    [save({ details: {} }), 'saved', 'neutral'],
    [save({ details: { outcome: 'sending' } }), 'sending', 'neutral'],
    [save({ details: { outcome: 'unknown' } }), 'no answer from KM2', 'danger'],
  ])('%#', (entry, label, tone) => {
    expect(saveOutcome(entry)).toEqual({ label, tone })
  })
})

describe('KM2SavesPanel', () => {
  it('lists the latest saves from the action log', async () => {
    vi.mocked(crewStateApi.log).mockResolvedValue({
      items: [save({}), save({ id: 'e2', dry_run: true, run_id: null, details: { external_ref: 'atlas-w40', folder: 'Projects.Atlas', connection: 'KM2 Finance' } }),
        save({ id: 'e3', action: 'followup:due_soon' })],
      meta: { total: 12, limit: 10, offset: 0 },
    })
    renderRoute(<KM2SavesPanel crewId="c1" />)
    const table = await screen.findByRole('table', { name: 'Recent KM2 saves' })
    expect(crewStateApi.log).toHaveBeenCalledWith('c1', { action: 'km2:save', limit: 10, offset: 0 }, expect.anything())
    const first = within(screen.getByText('Atlas weekly W41').closest('tr') as HTMLElement)
    expect(first.getByText('atlas-w41')).toBeTruthy()
    expect(first.getByText('created')).toBeTruthy()
    expect(first.getByRole('link', { name: 'open' }).getAttribute('href')).toBe('/runs/r1')
    const second = within(screen.getByText('atlas-w40').closest('tr') as HTMLElement)
    expect(second.getByText('dry run, not sent')).toBeTruthy()
    expect(second.getByText('Projects.Atlas')).toBeTruthy()
    expect(within(table).queryByText('followup:due_soon')).toBeNull()
    expect(screen.getByText(/Showing the latest 2 of 12/)).toBeTruthy()
  })

  it('shows nothing until the crew has saved something', async () => {
    vi.mocked(crewStateApi.log).mockResolvedValue({ items: [], meta: { total: 0, limit: 10, offset: 0 } })
    const { container } = renderRoute(<KM2SavesPanel crewId="c1" />)
    await waitFor(() => expect(crewStateApi.log).toHaveBeenCalled())
    expect(container.textContent).toBe('')
  })
})
