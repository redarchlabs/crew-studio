import { describe, expect, it } from 'vitest'
import { connectionsOf, describeDestination, newRow, splitRecipients, toInputs, toRows, validateRows, type DestinationRow } from './destinationsForm'
import { makeDestination } from './testUtils'

const row = (over: Partial<DestinationRow> = {}): DestinationRow => ({ kind: 'slack', connection_id: 'c1', recipients: '', folder: '', ...over })

describe('destinationsForm', () => {
  it('turns saved destinations into rows and back', () => {
    const rows = toRows([makeDestination({ kind: 'email', recipients: ['a@example.com', 'b@example.com'] })])
    expect(rows[0].recipients).toBe('a@example.com, b@example.com')
    expect(toInputs([...rows, row({ kind: 'km2', folder: ' f ' }), row({ recipients: 'ignored@example.com' })])).toEqual([
      { kind: 'email', connection_id: 'c-slack', recipients: ['a@example.com', 'b@example.com'], folder: '' },
      { kind: 'km2', connection_id: 'c1', recipients: [], folder: 'f' },
      { kind: 'slack', connection_id: 'c1', recipients: [], folder: '' },
    ])
    expect(splitRecipients(' a@x.io;b@x.io\nc@x.io  ')).toEqual(['a@x.io', 'b@x.io', 'c@x.io'])
  })

  it('picks connections by kind and starts new rows on one that exists', () => {
    const conns = [{ id: 'm', name: 'Mail', kind: 'email' }, { id: 'h', name: 'HTTP', kind: 'http' }]
    expect(connectionsOf(conns, 'email')).toHaveLength(1)
    expect(newRow(conns)).toEqual({ kind: 'email', connection_id: 'm', recipients: '', folder: '' })
    expect(newRow([])).toEqual({ kind: 'slack', connection_id: '', recipients: '', folder: '' })
  })

  it('checks rows like the server does', () => {
    const many = Array.from({ length: 51 }, (_, i) => `p${i}@example.com`).join(',')
    expect(validateRows([
      row({ connection_id: '' }),
      row({ kind: 'email' }),
      row({ kind: 'email', recipients: many }),
      row({ kind: 'email', recipients: 'ok@example.com, Evil <e@x.io>' }),
      row({ kind: 'email', recipients: 'nodot@localhost' }),
      row({ kind: 'km2', folder: 'not-a-uuid' }),
      row(),
      row(),
      row({ kind: 'km2', folder: 'AAAAAAAA-0000-4000-8000-000000000001' }),
    ])).toEqual({
      0: 'Choose a connection',
      1: 'List at least one email address',
      2: 'At most 50 addresses',
      3: 'Not a plain email address: Evil',
      4: 'Not a plain email address: nodot@localhost',
      5: 'The folder must be a KM2 folder id (a UUID), or empty for the default',
      7: 'Same as an earlier destination',
    })
  })

  it('describes destinations', () => {
    expect(describeDestination(makeDestination())).toBe('Slack: PMO Slack')
    expect(describeDestination(makeDestination({ kind: 'email', recipients: ['a@x.io'] }))).toBe('Email: PMO Slack → a@x.io')
    expect(describeDestination(makeDestination({ kind: 'km2' }))).toBe('KM2: PMO Slack (default folder)')
    expect(describeDestination(makeDestination({ kind: 'km2', folder: 'f1' }))).toBe('KM2: PMO Slack (folder f1)')
  })
})
