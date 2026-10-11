import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { ConnectionTool } from '../../api/types.crewstate'
import { renderRoute, setupApi } from '../crews/testUtils'
import { ConnectionsPage } from './ConnectionsPage'
import { changedTools, MCPToolsModal } from './MCPToolsModal'
import { KINDS, makeConnection } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')

const TS = '2026-10-10T07:00:00Z'
const tool = (over: Partial<ConnectionTool>): ConnectionTool => ({
  connection_id: 'c-mcp', name: 'search_notes', description: 'Searches the notes.', input_schema: { type: 'object' }, effect: 'reads',
  enabled: true, discovered_at: TS, updated_by_name: '', updated_at: TS, ...over,
})
const tools = [tool({}), tool({ name: 'delete_note', description: '<b>Deletes</b> a note.', effect: 'changes' })]
const mcp = makeConnection({ id: 'c-mcp', name: 'Notes MCP', kind: 'mcp', config: { url: 'https://mcp.example/mcp' } })

beforeEach(() => {
  setupApi()
  vi.mocked(connectionsApi.kinds).mockResolvedValue(KINDS)
  vi.mocked(connectionsApi.list).mockResolvedValue([mcp, makeConnection({ id: 'c-api', name: 'Ticket API' })])
  vi.mocked(connectionsApi.tools).mockResolvedValue(tools)
})
afterEach(cleanup)

const row = (name: string) => within(screen.getByText(name).closest('tr') as HTMLElement)

describe('MCP connection tools', () => {
  it('only MCP connections have tools; an administrator discovers them and sets what each does', async () => {
    vi.mocked(connectionsApi.discoverTools).mockResolvedValue({ tools: [...tools, tool({ name: 'new_tool', effect: 'changes' })], found: 4,
      skipped: ['"a b": the name must be 1 to 128 letters, digits, \'_\', \'.\' or \'-\''], review: ['new_tool'] })
    vi.mocked(connectionsApi.setTools).mockResolvedValue([tool({ effect: 'changes' }), tools[1]])
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Ticket API')
    expect(row('Ticket API').queryByRole('button', { name: /Tools/ })).toBeNull()
    fireEvent.click(row('Notes MCP').getByRole('button', { name: /Tools/ }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(await dialog.findByText('search_notes')).toBeTruthy()
    // The server's description is text, never markup
    expect(dialog.getByText('<b>Deletes</b> a note.')).toBeTruthy()
    expect(dialog.getAllByText('in chat')).toHaveLength(1)
    const save = dialog.getByRole('button', { name: 'Save' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)

    fireEvent.click(dialog.getByRole('button', { name: /Discover tools/ }))
    expect(await dialog.findByText('new_tool')).toBeTruthy()
    expect(dialog.getByText('1 tools were left out')).toBeTruthy()
    expect(dialog.getByText(/marked Changes until you check them: new_tool\./)).toBeTruthy()
    expect(connectionsApi.discoverTools).toHaveBeenCalledWith('c-mcp')

    fireEvent.change(dialog.getByLabelText('Effect of search_notes'), { target: { value: 'changes' } })
    fireEvent.click(dialog.getByLabelText('Enable delete_note'))
    expect(dialog.queryAllByText('in chat')).toHaveLength(0)
    expect(save.disabled).toBe(false)
    // Discovering again would lose the unsaved edits
    expect((dialog.getByRole('button', { name: /Discover tools/ }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(save)
    await waitFor(() => expect(connectionsApi.setTools).toHaveBeenCalledWith('c-mcp', [
      { name: 'search_notes', effect: 'changes', enabled: true }, { name: 'delete_note', effect: 'changes', enabled: false }]))
    await waitFor(() => expect(save.disabled).toBe(true))
  })

  it('people who only view connections see the tools but cannot change them', async () => {
    renderRoute(<MCPToolsModal connection={mcp} canManage={false} onClose={() => {}} />)
    const dialog = within(await screen.findByRole('dialog'))
    expect(await dialog.findByText('search_notes')).toBeTruthy()
    expect((dialog.getByLabelText('Effect of search_notes') as HTMLSelectElement).disabled).toBe(true)
    expect((dialog.getByLabelText('Enable search_notes') as HTMLInputElement).disabled).toBe(true)
    expect(dialog.queryByRole('button', { name: /Discover tools/ })).toBeNull()
    expect(dialog.queryByRole('button', { name: 'Save' })).toBeNull()
  })

  it('says when there are no tools yet, and when discovery or saving fails', async () => {
    vi.mocked(connectionsApi.tools).mockResolvedValue([])
    vi.mocked(connectionsApi.discoverTools).mockRejectedValue(new Error('down'))
    renderRoute(<MCPToolsModal connection={mcp} canManage onClose={() => {}} />)
    const dialog = within(await screen.findByRole('dialog'))
    expect(await dialog.findByText(/No tools yet\. Discover them/)).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: /Discover tools/ }))
    await waitFor(() => expect(connectionsApi.discoverTools).toHaveBeenCalled())
    expect(dialog.getByText(/No tools yet/)).toBeTruthy()
  })

  it('keeps the edit when saving fails', async () => {
    vi.mocked(connectionsApi.setTools).mockRejectedValue(new Error('nope'))
    renderRoute(<MCPToolsModal connection={mcp} canManage onClose={() => {}} />)
    const dialog = within(await screen.findByRole('dialog'))
    fireEvent.click(await dialog.findByLabelText('Enable search_notes'))
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(connectionsApi.setTools).toHaveBeenCalled())
    expect((dialog.getByLabelText('Enable search_notes') as HTMLInputElement).checked).toBe(false)
  })

  it('changedTools lists only what differs from what is saved', () => {
    expect(changedTools(tools, { search_notes: { effect: 'reads', enabled: true }, delete_note: { effect: 'reads', enabled: true } }))
      .toEqual([{ name: 'delete_note', effect: 'reads', enabled: true }])
    expect(changedTools(tools, {})).toEqual([])
  })
})
