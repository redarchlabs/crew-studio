import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { ConnectionKind } from '../../api/types.crewstate'
import { renderRoute, setupApi } from '../crews/testUtils'
import { ConnectionEditor } from './ConnectionEditor'
import { checkValue } from './connectionForm'
import { makeConnection } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')

const EMAIL_KIND: ConnectionKind = {
  kind: 'email', label: 'Email (SMTP)', testable: true,
  fields: [
    { key: 'host', label: 'SMTP host', input: 'text', secret: false, required: true },
    { key: 'port', label: 'Port', input: 'port', secret: false, required: true },
    { key: 'security', label: 'Security', input: 'select', secret: false, required: false, options: ['starttls', 'tls', 'none'] },
    { key: 'from_address', label: 'From address', input: 'email', secret: false, required: true },
    { key: 'password', label: 'Password', input: 'text', secret: true, required: false },
  ],
}

beforeEach(() => setupApi())
afterEach(cleanup)

describe('select settings', () => {
  it('checks the value is one of the options', () => {
    const field = EMAIL_KIND.fields[2]
    expect(checkValue(field, 'TLS')).toBeUndefined()
    expect(checkValue(field, 'ssl')).toBe('Must be one of starttls, tls, none')
    expect(checkValue({ ...field, options: undefined }, 'x')).toBe('Must be one of ')
  })

  it('offers the options in the editor and saves the choice', async () => {
    vi.mocked(connectionsApi.create).mockResolvedValue(makeConnection({ kind: 'email' }))
    const onSaved = vi.fn()
    renderRoute(<ConnectionEditor kinds={[EMAIL_KIND]} connection={null} onClose={() => {}} onSaved={onSaved} />, { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'PMO Mail' } })
    fireEvent.change(screen.getByLabelText('SMTP host'), { target: { value: 'smtp.example.com' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '465' } })
    const security = screen.getByLabelText('Security') as HTMLSelectElement
    expect(screen.getByRole('option', { name: 'Default (starttls)' })).toBeTruthy()
    fireEvent.change(security, { target: { value: 'tls' } })
    fireEvent.change(screen.getByLabelText('From address'), { target: { value: 'pmo@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(connectionsApi.create).toHaveBeenCalledWith(expect.objectContaining({
      config: { host: 'smtp.example.com', port: '465', security: 'tls', from_address: 'pmo@example.com' },
    })))
  })

  it('asks for a choice when a select is required', async () => {
    const kind = { ...EMAIL_KIND, fields: [{ ...EMAIL_KIND.fields[2], required: true }] }
    renderRoute(<ConnectionEditor kinds={[kind]} connection={null} onClose={() => {}} onSaved={() => {}} />)
    expect(await screen.findByRole('option', { name: 'Choose…' })).toBeTruthy()
    const empty = { ...EMAIL_KIND, fields: [{ ...EMAIL_KIND.fields[2], options: [] }] }
    cleanup()
    renderRoute(<ConnectionEditor kinds={[empty]} connection={null} onClose={() => {}} onSaved={() => {}} />)
    expect(await screen.findByRole('option', { name: 'Default' })).toBeTruthy()
  })
})
