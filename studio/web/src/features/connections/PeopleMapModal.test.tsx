import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionsApi } from '../../api/endpoints.crewstate'
import { peopleApi } from '../../api/endpoints.trackers'
import type { ConnectionKind } from '../../api/types.crewstate'
import type { PeopleMapEntry } from '../../api/types.trackers'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { checkValue } from './connectionForm'
import { ConnectionsPage } from './ConnectionsPage'
import { KINDS, makeConnection } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')
vi.mock('../../api/endpoints.trackers')

const TS = '2026-10-05T07:00:00Z'
const person = (over: Partial<PeopleMapEntry>): PeopleMapEntry => ({
  connection_id: 'c-jira', account_id: 'acc-sam', display_name: 'Sam Lee', email: 'sam@example.com', user_id: 'u-sam', user_name: 'Sam',
  link: 'email', seen_at: TS, updated_at: TS, ...over,
})
const people = [person({}), person({ account_id: 'acc-dana', display_name: 'Dana', email: '', user_id: null, user_name: '', link: '' })]
const users = [{ id: 'u-sam', name: 'Sam', email: 'sam@example.com' }, { id: 'u-dana', name: 'Dana R', email: '' }]

const JIRA_WITH_TOGGLE: ConnectionKind = {
  ...KINDS.kinds[0], testable: true,
  fields: [...KINDS.kinds[0].fields, { key: 'allow_create', label: 'Let crews create issues', input: 'toggle', secret: false, required: false, hint: 'Off by default.' },
    { key: 'cloud_id', label: 'Cloud ID', input: 'cloud_id', secret: false, required: false }],
}

beforeEach(() => {
  setupApi()
  vi.mocked(connectionsApi.kinds).mockResolvedValue({ ...KINDS, kinds: [JIRA_WITH_TOGGLE, ...KINDS.kinds.slice(1)] })
  vi.mocked(connectionsApi.list).mockResolvedValue([
    makeConnection({ id: 'c-jira', name: 'Acme Jira', kind: 'jira', config: { site_url: 'https://acme.atlassian.net', email: 'bot@acme.example' } }),
    makeConnection({ id: 'c-api', name: 'Ticket API' }),
  ])
  vi.mocked(peopleApi.list).mockResolvedValue({ people, users })
})
afterEach(cleanup)

const row = (name: string) => within(screen.getByText(name).closest('tr') as HTMLElement)

describe('People map', () => {
  it('only tracker connections have one; it lists accounts and links them to Studio users', async () => {
    vi.mocked(peopleApi.link).mockResolvedValue(person({ account_id: 'acc-dana', display_name: 'Dana', user_id: 'u-dana', user_name: 'Dana R', link: 'manual' }))
    vi.mocked(peopleApi.sync).mockResolvedValue({ people: [...people, person({ account_id: 'acc-new', display_name: 'New Person', link: '' })], users, read: 3, linked: 1 })
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Ticket API')
    expect(row('Ticket API').queryByRole('button', { name: /People map/ })).toBeNull()
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /People map/ }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(await dialog.findByText('Sam Lee')).toBeTruthy()
    expect(dialog.getByText('by email')).toBeTruthy()
    expect(dialog.getByText('hidden by the tracker')).toBeTruthy()
    fireEvent.change(dialog.getByLabelText('Studio user for Dana'), { target: { value: 'u-dana' } })
    await waitFor(() => expect(peopleApi.link).toHaveBeenCalledWith('c-jira', 'acc-dana', 'u-dana'))
    expect(await dialog.findByText('by hand')).toBeTruthy()
    fireEvent.change(dialog.getByLabelText('Studio user for Sam Lee'), { target: { value: '' } })
    await waitFor(() => expect(peopleApi.link).toHaveBeenCalledWith('c-jira', 'acc-sam', null))
    fireEvent.click(dialog.getByRole('button', { name: /Read people from the tracker/ }))
    expect(await dialog.findByText('New Person')).toBeTruthy()
    fireEvent.click(dialog.getAllByRole('button', { name: 'Close' }).at(-1)!)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('is read-only without connections.manage, and reports failures', async () => {
    vi.mocked(peopleApi.list).mockResolvedValue({ people: [], users })
    renderRoute(<ConnectionsPage />, { perms: without('connections.manage') })
    await screen.findByText('Acme Jira')
    expect(screen.queryByRole('button', { name: /Read people/ })).toBeNull()
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /People map/ }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(await dialog.findByText(/No people yet/)).toBeTruthy()
    cleanup()
    vi.mocked(peopleApi.list).mockResolvedValue({ people, users })
    vi.mocked(peopleApi.link).mockRejectedValue(new Error('offline'))
    vi.mocked(peopleApi.sync).mockRejectedValue(new Error('offline'))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Acme Jira')
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /People map/ }))
    const d2 = within(await screen.findByRole('dialog'))
    fireEvent.change(await d2.findByLabelText('Studio user for Dana'), { target: { value: 'u-dana' } })
    await waitFor(() => expect(peopleApi.link).toHaveBeenCalled())
    fireEvent.click(d2.getByRole('button', { name: /Read people from the tracker/ }))
    await waitFor(() => expect(peopleApi.sync).toHaveBeenCalled())
    expect(d2.getByText('not linked')).toBeTruthy() // unchanged after the failures
  })
})

describe('Tracker connection settings', () => {
  it('shows on/off settings as a switch and checks cloud ids', async () => {
    vi.mocked(connectionsApi.update).mockImplementation(async (_id, body) => makeConnection({ id: 'c-jira', name: body.name, kind: 'jira', config: body.config }))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Acme Jira')
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /Edit/ }))
    const dialog = within(await screen.findByRole('dialog'))
    fireEvent.change(dialog.getByLabelText(/Cloud ID/), { target: { value: 'nope' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await dialog.findByText('Must be an Atlassian cloud id (a UUID)')).toBeTruthy()
    fireEvent.change(dialog.getByLabelText(/Cloud ID/), { target: { value: '' } })
    fireEvent.click(dialog.getByRole('switch', { name: /Let crews create issues/ }))
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(connectionsApi.update).toHaveBeenCalledWith('c-jira', expect.objectContaining({
      config: { site_url: 'https://acme.atlassian.net', email: 'bot@acme.example', allow_create: 'true' },
    })))
    const toggle = JIRA_WITH_TOGGLE.fields.find((f) => f.input === 'toggle')!
    expect(checkValue(toggle, 'true')).toBeUndefined()
    expect(checkValue(toggle, 'maybe')).toBe('Must be on or off')
  })
})
