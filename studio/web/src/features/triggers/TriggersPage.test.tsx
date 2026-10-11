import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { triggersApi } from '../../api/endpoints'
import { makeTrigger, renderRoute, setupApi, without } from '../crews/testUtils'
import { TriggersPage } from './TriggersPage'

vi.mock('../../api/endpoints')

const hourAgo = () => new Date(Date.now() - 3600_000).toISOString()

beforeEach(() => {
  setupApi()
  vi.mocked(triggersApi.list).mockResolvedValue([
    makeTrigger({ id: 'b', name: 'Zed hook', description: 'Fires on tickets', enabled: true, last_fired_at: hourAgo(), fire_count: 12345, callback_url: 'https://cb.example/x', token_prefix: 'k1k1', target_name: 'Research', area_ids: ['area-1'] }),
    makeTrigger({ id: 'a', name: 'Alpha hook', enabled: false, last_fired_at: null, fire_count: 0, callback_url: null, token_prefix: 'k2k2' }),
  ])
})
afterEach(cleanup)

const names = () => screen.getAllByRole('link').filter((l) => /^\/triggers\/[ab]$/.test(l.getAttribute('href') ?? '')).map((l) => l.textContent)
const row = (name: string) => screen.getByRole('link', { name }).closest('tr') as HTMLElement

describe('TriggersPage', () => {
  it('lists triggers sorted by name', async () => {
    renderRoute(<TriggersPage />)
    await screen.findByText('Alpha hook')
    expect(names()).toEqual(['Alpha hook', 'Zed hook'])
    expect(screen.getByRole('link', { name: 'Zed hook' }).getAttribute('href')).toBe('/triggers/b')
  })

  it('shows status, last fired, fire count, callback, token prefix and crew', async () => {
    renderRoute(<TriggersPage />)
    await screen.findByText('Alpha hook')
    const zed = within(row('Zed hook'))
    expect(zed.getByText('enabled')).toBeTruthy()
    expect(zed.getByText('1h ago')).toBeTruthy()
    expect(zed.getByText('12,345')).toBeTruthy()
    expect(zed.getByText('set').closest('span')?.getAttribute('title')).toBe('https://cb.example/x')
    expect(zed.getByText('cst_k1k1_…')).toBeTruthy()
    expect(zed.getByText('Research').closest('a')?.getAttribute('href')).toBe('/crews/crew-1')
    expect(zed.getByText('Fires on tickets')).toBeTruthy()
    expect(await zed.findByText('Finance')).toBeTruthy()
  })

  it('shows disabled, never, and no callback for an idle trigger', async () => {
    renderRoute(<TriggersPage />)
    await screen.findByText('Alpha hook')
    const alpha = within(row('Alpha hook'))
    expect(alpha.getByText('disabled')).toBeTruthy()
    expect(alpha.getByText('never')).toBeTruthy()
    expect(alpha.getByText('—')).toBeTruthy()
    expect(alpha.getByText('0')).toBeTruthy()
  })

  it('offers creation to managers', async () => {
    renderRoute(<TriggersPage />)
    await screen.findByText('Alpha hook')
    expect(screen.getByRole('link', { name: /New trigger/ }).getAttribute('href')).toBe('/triggers/new')
  })

  it('hides creation without triggers.manage', async () => {
    renderRoute(<TriggersPage />, { perms: without('triggers.manage') })
    await screen.findByText('Alpha hook')
    expect(screen.queryByRole('link', { name: /New trigger/ })).toBeNull()
  })

  it('shows the empty state with a create action for managers', async () => {
    vi.mocked(triggersApi.list).mockResolvedValue([])
    renderRoute(<TriggersPage />)
    expect(await screen.findByText('No triggers yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Create a trigger' }).getAttribute('href')).toBe('/triggers/new')
  })

  it('empty state has no create action for non-managers', async () => {
    vi.mocked(triggersApi.list).mockResolvedValue([])
    renderRoute(<TriggersPage />, { perms: without('triggers.manage') })
    expect(await screen.findByText('No triggers yet')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /Create a trigger/ })).toBeNull()
  })

  it('shows an error and retries', async () => {
    vi.mocked(triggersApi.list).mockRejectedValueOnce(new ApiError(500, { code: 'x', message: 'triggers failed' }))
    renderRoute(<TriggersPage />)
    expect(await screen.findByText('triggers failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('Alpha hook')).toBeTruthy()
  })
})
