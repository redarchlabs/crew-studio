import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { ConnectionKind, KM2FolderList } from '../../api/types.crewstate'
import { renderRoute, setupApi } from '../crews/testUtils'
import { ConnectionEditor } from './ConnectionEditor'
import { KM2FolderSelect, NO_DEFAULT } from './KM2FolderSelect'
import { MAX_WRITE_FOLDERS, PICK_LATER_HINT } from './KM2FolderFields'
import { clearFolderCache, folderLabel, loadFolders, splitIds, stateOf, type FolderState } from './km2Folders'
import { makeConnection } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')

const PROJECTS = '6a1f0000-0000-4000-8000-000000000001'
const ATLAS = '6a1f0000-0000-4000-8000-000000000002'
const HR = '6a1f0000-0000-4000-8000-000000000003'
const GONE = '6a1f0000-0000-4000-8000-0000000000ff'

function folderList(over: Partial<KM2FolderList> = {}): KM2FolderList {
  return {
    ok: true, message: 'The key sees 3 folders.', folder_count: 3, warnings: [],
    folders: [
      { id: PROJECTS, name: 'Projects', path: 'Projects', parent_id: null },
      { id: ATLAS, name: 'Atlas', path: 'Projects.Atlas', parent_id: PROJECTS },
      { id: HR, name: 'HR', path: '', parent_id: null },
    ],
    allowed: { default_folder_id: ATLAS, write_folder_ids: [PROJECTS] },
    ...over,
  }
}

const KM2_KIND: ConnectionKind = {
  kind: 'km2', label: 'KM2 (Knowledge Manager)', testable: true,
  fields: [
    { key: 'base_url', label: 'Base URL', input: 'url', secret: false, required: true },
    { key: 'default_folder_id', label: 'Default folder for saves', input: 'uuid', secret: false, required: false, hint: 'Where saves go.' },
    { key: 'write_folder_ids', label: 'Other folders crews may save to', input: 'uuid_list', secret: false, required: false },
    { key: 'api_key', label: 'Profile API key', input: 'text', secret: true, required: true },
  ],
}

beforeEach(() => {
  setupApi()
  clearFolderCache()
})
afterEach(cleanup)

describe('folder lists', () => {
  it('lists each key once, and tries a failed list again', async () => {
    const load = vi.fn().mockResolvedValueOnce(folderList({ ok: false, message: 'no knowledge:read' })).mockResolvedValue(folderList())
    expect((await loadFolders('c1', load)).ok).toBe(false)
    expect((await loadFolders('c1', load)).ok).toBe(true)
    await loadFolders('c1', load)
    expect(load).toHaveBeenCalledTimes(2)
    const failing = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(folderList())
    await expect(loadFolders('c2', failing)).rejects.toThrow('offline')
    expect((await loadFolders('c2', failing)).ok).toBe(true)
  })

  it('names folders by path and marks the ones the key cannot see', () => {
    const list = folderList()
    expect(folderLabel(list, ATLAS.toUpperCase())).toBe('Projects.Atlas')
    expect(folderLabel(list, HR)).toBe('HR')
    expect(folderLabel(list, GONE)).toBe(`${GONE} (not visible to this key)`)
    expect(splitIds(` ${ATLAS}, ${ATLAS}\n${HR},`)).toEqual([ATLAS, HR])
    expect(stateOf(undefined)).toEqual({ status: 'failed', message: 'KM2 did not list the folders.' })
    expect(stateOf(folderList({ ok: false, message: '' }))).toEqual({ status: 'failed', message: 'KM2 did not list the folders.' })
  })
})

describe('KM2FolderSelect', () => {
  const show = (state: FolderState | null, value = '') => {
    const onChange = vi.fn()
    render(<KM2FolderSelect state={state} value={value} onChange={onChange} what="reports" />)
    return onChange
  }

  it('offers the default folder first, then the other allowed folders, by path', () => {
    const onChange = show({ status: 'ready', list: folderList() })
    const select = screen.getByLabelText('Folder') as HTMLSelectElement
    expect([...select.options].map((o) => o.text)).toEqual(["Connection's default folder (Projects.Atlas)", 'Projects'])
    fireEvent.change(select, { target: { value: PROJECTS } })
    expect(onChange).toHaveBeenCalledWith(PROJECTS)
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('warns when the connection has no default folder and none is picked', () => {
    show({ status: 'ready', list: folderList({ allowed: { default_folder_id: '', write_folder_ids: [PROJECTS] } }) })
    expect(screen.getByRole('option', { name: NO_DEFAULT })).toBeTruthy()
    expect(screen.getByRole('status').textContent).toContain('no default folder, so reports cannot be filed')
  })

  it('keeps a saved folder that is not offered, saying why', () => {
    show({ status: 'ready', list: folderList() }, GONE)
    expect(screen.getByRole('option', { name: `${GONE} (not visible to this key)` })).toBeTruthy()
    cleanup()
    show({ status: 'ready', list: folderList() }, HR)
    expect(screen.getByRole('option', { name: 'HR (not allowed by the connection)' })).toBeTruthy()
  })

  it('falls back to typing an id when the folders cannot be listed, or before a connection is chosen', () => {
    const onChange = show({ status: 'failed', message: 'KM2 accepted the key, but it may not list folders.' })
    const input = screen.getByLabelText('Folder (optional)')
    expect(screen.getByText(/Could not list the folders: KM2 accepted the key/)).toBeTruthy()
    fireEvent.change(input, { target: { value: ATLAS } })
    expect(onChange).toHaveBeenCalledWith(ATLAS)
    cleanup()
    show(null)
    expect(screen.queryByText(/Could not list/)).toBeNull()
    expect(screen.getByLabelText('Folder (optional)')).toBeTruthy()
    cleanup()
    show({ status: 'loading' })
    expect((screen.getByLabelText('Folder') as HTMLSelectElement).disabled).toBe(true)
  })
})

describe('the KM2 connection form', () => {
  const editor = (connection = makeConnection({ kind: 'km2', secret_keys: ['api_key'], config: { base_url: 'https://km2.example.com', default_folder_id: ATLAS, write_folder_ids: `${PROJECTS},${GONE}` } })) => {
    const onSaved = vi.fn()
    renderRoute(<ConnectionEditor kinds={[KM2_KIND]} connection={connection} onClose={() => {}} onSaved={onSaved} />, { me: { all_areas: false, area_ids: ['area-1'] } })
    return onSaved
  }

  it('picks the folders from lists once the key is saved, and saves the ids', async () => {
    vi.mocked(connectionsApi.km2Folders).mockResolvedValue(folderList())
    vi.mocked(connectionsApi.update).mockResolvedValue(makeConnection({ kind: 'km2' }))
    const onSaved = editor()
    const def = await screen.findByLabelText('Default folder for saves') as HTMLSelectElement
    await waitFor(() => expect(def.disabled).toBe(false))
    expect(def.value).toBe(ATLAS)
    expect([...def.options].map((o) => o.text)).toEqual(['None (saving off)', 'Projects', 'Projects.Atlas', 'HR'])
    const box = within(screen.getByTestId('write-folders'))
    expect((box.getByLabelText('Projects') as HTMLInputElement).checked).toBe(true)
    expect((box.getByLabelText(`${GONE} (not visible to this key)`) as HTMLInputElement).checked).toBe(true)
    fireEvent.click(box.getByLabelText(`${GONE} (not visible to this key)`))
    fireEvent.click(box.getByLabelText('HR'))
    fireEvent.change(def, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(connectionsApi.update).toHaveBeenCalledWith('conn-1', expect.objectContaining({
      config: expect.objectContaining({ write_folder_ids: `${PROJECTS},${HR}` }),
    })))
    expect(vi.mocked(connectionsApi.update).mock.calls[0][1].config.default_folder_id).toBeUndefined()
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
  })

  it('keeps a default folder the key cannot see, and stops at the folder limit', async () => {
    const many = Array.from({ length: MAX_WRITE_FOLDERS + 1 }, (_, i) => ({ id: `6a1f0000-0000-4000-8000-${String(i).padStart(12, '0')}`, name: `F${i}`, path: `F${i}`, parent_id: null }))
    vi.mocked(connectionsApi.km2Folders).mockResolvedValue(folderList({ folders: many }))
    editor(makeConnection({ kind: 'km2', secret_keys: ['api_key'], config: { base_url: 'https://k', default_folder_id: GONE,
      write_folder_ids: many.slice(0, MAX_WRITE_FOLDERS).map((f) => f.id).join(',') } }))
    expect(await screen.findByRole('option', { name: `${GONE} (not visible to this key)` })).toBeTruthy()
    const box = within(screen.getByTestId('write-folders'))
    expect((box.getByLabelText(`F${MAX_WRITE_FOLDERS}`) as HTMLInputElement).disabled).toBe(true)
    expect(screen.getByText(`At most ${MAX_WRITE_FOLDERS} folders.`)).toBeTruthy()
  })

  it('types the folder ids on a new connection, or when the list cannot load, saying why', async () => {
    editor(null as never)
    expect(await screen.findByText(PICK_LATER_HINT, { exact: false })).toBeTruthy()
    expect(screen.getByLabelText('Default folder for saves (optional)')).toBeTruthy()
    expect(connectionsApi.km2Folders).not.toHaveBeenCalled()
    cleanup()
    vi.mocked(connectionsApi.km2Folders).mockResolvedValue(folderList({ ok: false, message: 'KM2 accepted the key, but it may not list folders.' }))
    editor()
    expect(await screen.findByText(/Could not list the folders, so type their ids: KM2 accepted the key/)).toBeTruthy()
    expect((screen.getByLabelText('Other folders crews may save to (optional)') as HTMLInputElement).value).toBe(`${PROJECTS},${GONE}`)
    cleanup()
    clearFolderCache()
    vi.mocked(connectionsApi.km2Folders).mockResolvedValue(folderList({ folders: [] }))
    editor(makeConnection({ kind: 'km2', secret_keys: ['api_key'], config: { base_url: 'https://k' } }))
    expect(await screen.findByText('The key sees no folders.')).toBeTruthy()
  })
})
