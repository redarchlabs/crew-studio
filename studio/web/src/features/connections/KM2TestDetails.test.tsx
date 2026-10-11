import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { ConnectionKind, ConnectionTestResult } from '../../api/types.crewstate'
import { renderRoute, setupApi } from '../crews/testUtils'
import { checkValue, validateDraft } from './connectionForm'
import { ConnectionsPage } from './ConnectionsPage'
import { KM2TestDetails, saveFolders } from './KM2TestDetails'
import { KINDS, makeConnection } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')

const ATLAS = '6a1f0000-0000-4000-8000-000000000002'
const PROJECTS = '6a1f0000-0000-4000-8000-000000000001'
const HR = '6a1f0000-0000-4000-8000-000000000003'

const KM2_KIND: ConnectionKind = {
  kind: 'km2',
  label: 'KM2 (Knowledge Manager)',
  testable: true,
  fields: [
    { key: 'base_url', label: 'Base URL', input: 'url', secret: false, required: true },
    { key: 'default_folder_id', label: 'Default folder for saves', input: 'uuid', secret: false, required: false },
    { key: 'write_folder_ids', label: 'Other folders crews may save to', input: 'uuid_list', secret: false, required: false },
    { key: 'api_key', label: 'Profile API key', input: 'text', secret: true, required: true },
  ],
}

const result = (over: Partial<ConnectionTestResult> = {}): ConnectionTestResult => ({
  supported: true, ok: true, message: 'Connected: the key sees 3 folders.', duration_ms: 9, folder_count: 3,
  folders: [
    { id: PROJECTS, name: 'Projects', path: 'Projects', parent_id: null },
    { id: ATLAS, name: 'Atlas', path: 'Projects.Atlas', parent_id: PROJECTS },
    { id: HR, name: 'HR', path: '', parent_id: null },
  ],
  warnings: [],
  ...over,
})

afterEach(cleanup)

describe('KM2 folder settings', () => {
  const f = (input: 'uuid' | 'uuid_list') => ({ key: 'k', label: 'K', input, secret: false, required: false })
  it.each([
    ['uuid', ATLAS.toUpperCase(), undefined],
    ['uuid', 'nope', 'Must be a KM2 folder id (a UUID)'],
    ['uuid_list', `${ATLAS}, ${PROJECTS} ${HR}`, undefined],
    ['uuid_list', `${ATLAS},x`, '"x" is not a KM2 folder id (a UUID)'],
  ] as const)('%s %j', (input, value, want) => {
    expect(checkValue(f(input), value)).toBe(want)
  })

  it('refuses more than 50 folders', () => {
    const many = Array.from({ length: 51 }, (_, i) => `6a1f0000-0000-4000-8000-${String(i).padStart(12, '0')}`).join(',')
    expect(checkValue(f('uuid_list'), many)).toBe('List at most 50 folders')
  })

  it('validates a KM2 draft', () => {
    const errors = validateDraft({ name: 'KM2', kind: 'km2', config: { base_url: 'https://km2.example', default_folder_id: 'x' }, area_ids: ['a'], enabled: true, secrets: {} }, KM2_KIND, true)
    expect(errors['config.default_folder_id']).toBe('Must be a KM2 folder id (a UUID)')
  })

  it('reads the save folders from the settings', () => {
    expect(saveFolders({ default_folder_id: ATLAS.toUpperCase(), write_folder_ids: `${PROJECTS}, ${ATLAS},` })).toEqual({ defaultId: ATLAS, listed: [PROJECTS] })
    expect(saveFolders({})).toEqual({ defaultId: '', listed: [] })
  })
})

describe('KM2TestDetails', () => {
  it('lists the visible folders and marks where crews may save', () => {
    render(<KM2TestDetails result={result({ folder_count: 5 })} config={{ default_folder_id: ATLAS, write_folder_ids: PROJECTS }} />)
    expect(screen.getByText('Folders the key can see (5)')).toBeTruthy()
    const atlas = within(screen.getByText('Projects.Atlas').closest('li') as HTMLElement)
    expect(atlas.getByText('default for saves')).toBeTruthy()
    expect(within(screen.getByText('Projects').closest('li') as HTMLElement).getByText('crews may save')).toBeTruthy()
    expect(within(screen.getByText(HR).closest('li') as HTMLElement).getByText('HR')).toBeTruthy()
    expect(screen.getByText('and 2 more')).toBeTruthy()
  })

  it('shows warnings, and nothing else when the key sees no folders', () => {
    render(<KM2TestDetails result={result({ folders: [], folder_count: 0, warnings: ['No folders are set for saving.'] })} config={{}} />)
    expect(screen.getByText('No folders are set for saving.')).toBeTruthy()
    expect(screen.queryByText(/Folders the key can see/)).toBeNull()
  })

  it('copes with a result without lists', () => {
    render(<KM2TestDetails result={{ supported: true, ok: false, message: 'x', duration_ms: 1 }} config={{}} />)
    expect(screen.getByLabelText('KM2 test details').textContent).toBe('')
  })
})

describe('ConnectionsPage with a KM2 connection', () => {
  beforeEach(() => {
    setupApi()
    vi.mocked(connectionsApi.kinds).mockResolvedValue({ ...KINDS, kinds: [...KINDS.kinds, KM2_KIND] })
    vi.mocked(connectionsApi.list).mockResolvedValue([makeConnection({ id: 'c-km2', name: 'KM2 Finance', kind: 'km2', secret_keys: ['api_key'],
      config: { base_url: 'https://km2.example', default_folder_id: ATLAS } })])
  })

  it('shows warnings even when the key sees no folders', async () => {
    vi.mocked(connectionsApi.test).mockResolvedValueOnce(result({ folders: undefined, folder_count: 0, warnings: ['The key cannot see folder x.'] }))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('KM2 Finance')
    fireEvent.click(screen.getByRole('button', { name: /Test/ }))
    expect(await screen.findByText('The key cannot see folder x.')).toBeTruthy()
  })

  it('shows the folders a test found', async () => {
    vi.mocked(connectionsApi.test).mockResolvedValueOnce(result())
    renderRoute(<ConnectionsPage />)
    await screen.findByText('KM2 Finance')
    fireEvent.click(screen.getByRole('button', { name: /Test/ }))
    expect(await screen.findByText('Connected: the key sees 3 folders.')).toBeTruthy()
    expect(screen.getByText('Folders the key can see (3)')).toBeTruthy()
    expect(screen.getByText('default for saves')).toBeTruthy()
  })
})
