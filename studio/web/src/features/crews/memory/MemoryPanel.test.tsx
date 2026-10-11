import { cleanup, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { memoryApi } from '../../../api/endpoints.memory'
import { renderRoute, setupApi, without } from '../testUtils'
import { MemoryPanel } from './MemoryPanel'
import { makeSettingsView } from './testUtils'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.memory')

beforeEach(() => {
  setupApi()
  vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView())
  vi.mocked(memoryApi.list).mockResolvedValue({ items: [], meta: { total: 0, limit: 25, offset: 0 } })
  vi.mocked(memoryApi.scopes).mockResolvedValue([])
})
afterEach(cleanup)

const render = (perms?: Parameters<typeof without>) => renderRoute(<MemoryPanel crewId="c1" llms={[]} />, perms ? { perms: without(...perms) } : {})

describe('MemoryPanel', () => {
  it('shows the settings and the memories, editable with crews.edit and state.manage', async () => {
    render()
    expect(await screen.findByRole('button', { name: 'Save memory settings' })).toBeTruthy()
    expect(await screen.findByRole('button', { name: /Add a memory/ })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'How memory works' }).getAttribute('href')).toBe('/help/memory')
  })

  it('needs crews.edit as well as state.manage to change the settings', async () => {
    render(['crews.edit'])
    expect(await screen.findByRole('button', { name: /Add a memory/ })).toBeTruthy()
    await screen.findByLabelText('Memory')
    expect(screen.queryByRole('button', { name: 'Save memory settings' })).toBeNull()
  })

  it('is read-only with state.view alone', async () => {
    render(['state.manage'])
    await screen.findByLabelText('Memory')
    expect(await screen.findByText('No memories yet')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Save memory settings' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Add a memory/ })).toBeNull()
  })
})
