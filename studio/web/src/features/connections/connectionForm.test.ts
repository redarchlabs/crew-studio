import { describe, expect, it } from 'vitest'
import type { ConnectionKind } from '../../api/types.crewstate'
import { checkValue, draftFromConnection, errorKeys, newDraft, secretLabel, secretsToSend, serverErrors, toInput, validateDraft, validateSecrets } from './connectionForm'
import { HTTP_KIND, JIRA_KIND, makeConnection } from './testUtils'

describe('checkValue', () => {
  const f = (input: ConnectionKind['fields'][number]['input']) => ({ key: 'k', label: 'K', input, secret: false, required: false })
  it.each([
    ['url', 'https://x.example', undefined],
    ['url', 'http://x.example', undefined],
    ['url', 'nope', 'Must be a full URL, such as https://example.com'],
    ['url', 'ftp://x.example', 'Must be an http or https URL'],
    ['url', 'https://u:p@x.example', 'Must not contain a user name or password'],
    ['https_url', 'http://x.example/hook', 'Must be an https URL'],
    ['https_url', 'http://127.0.0.1:8795/hook', undefined],
    ['https_url', 'http://localhost/hook', undefined],
    ['https_url', 'http://[::1]:80/hook', undefined],
    ['email', 'a@b.example', undefined],
    ['email', 'nope', 'Must be an email address'],
    ['port', '587', undefined],
    ['port', '70000', 'Must be a port number (1 to 65535)'],
    ['header', 'X-API-Key', undefined],
    ['header', 'Bad Header:', 'Must be an HTTP header name, such as Authorization'],
    ['text', 'two\nlines', 'Must be one line'],
    ['text', '   ', undefined],
  ] as const)('%s %j', (input, value, want) => {
    expect(checkValue(f(input), value)).toBe(want)
  })

  it('checks a required prefix (KM2 keys)', () => {
    const key = { ...f('text'), secret: true, prefix: 'km2_' }
    expect(checkValue(key, 'lin_api_1')).toBe('Must start with km2_')
    expect(checkValue(key, ' km2_abc ')).toBeUndefined()
  })
})

describe('validateDraft', () => {
  it('requires a name, areas and required settings', () => {
    const errors = validateDraft(newDraft('jira', []), JIRA_KIND, true)
    expect(errors).toEqual({ name: 'Name is required', area_ids: 'Pick at least one functional area', 'config.site_url': 'Required', 'config.email': 'Required' })
  })

  it('checks typed secrets only when creating', () => {
    const draft = { ...newDraft('http', ['area-1']), name: 'API', config: { base_url: 'https://x.example' }, secrets: { auth_value: 'a\nb' } }
    expect(validateDraft(draft, HTTP_KIND, true)).toEqual({ 'secrets.auth_value': 'Must be one line' })
    expect(validateDraft(draft, HTTP_KIND, false)).toEqual({})
  })
})

describe('validateSecrets', () => {
  it('needs at least one secret and every required one when replacing', () => {
    expect(validateSecrets({}, JIRA_KIND, true)).toEqual({ 'secrets.api_token': 'Required', secrets: 'Enter at least one secret' })
    expect(validateSecrets({ api_token: 't' }, JIRA_KIND, true)).toEqual({})
    expect(validateSecrets({}, JIRA_KIND, false)).toEqual({})
  })
})

describe('toInput', () => {
  it('trims, drops empty values and sends secrets only when creating', () => {
    const draft = { ...newDraft('http', ['area-1']), name: ' API ', config: { base_url: 'https://x.example/ ', auth_header: ' ' }, secrets: { auth_value: ' s ' } }
    expect(toInput(draft, HTTP_KIND, true)).toEqual({ name: 'API', kind: 'http', config: { base_url: 'https://x.example' }, area_ids: ['area-1'], enabled: true, secrets: { auth_value: 's' } })
    expect(toInput(draft, HTTP_KIND, false)).not.toHaveProperty('secrets')
    expect(toInput({ ...draft, secrets: {} }, HTTP_KIND, true)).not.toHaveProperty('secrets')
  })

  it('secretsToSend keeps only typed secrets of the kind', () => {
    expect(secretsToSend({ auth_value: ' v ', base_url: 'x' }, HTTP_KIND)).toEqual({ auth_value: 'v' })
  })
})

describe('draftFromConnection and secretLabel', () => {
  it('copies settings but never secrets', () => {
    const c = makeConnection({ config: { base_url: 'https://x.example' } })
    const draft = draftFromConnection(c)
    expect(draft).toEqual({ name: c.name, kind: 'http', config: { base_url: 'https://x.example' }, area_ids: ['area-1'], enabled: true, secrets: {} })
    draft.config.base_url = 'changed'
    expect(c.config.base_url).toBe('https://x.example')
  })

  it('labels secret keys', () => {
    expect(secretLabel(JIRA_KIND, 'api_token')).toBe('API token')
    expect(secretLabel(undefined, 'webhook_url or bot_token')).toBe('webhook_url or bot_token')
  })
})

describe('serverErrors', () => {
  it('puts known field details on their fields and keeps the rest general', async () => {
    const { ApiError } = await import('../../api/client')
    const err = new ApiError(422, { code: 'validation_failed', message: 'Please fix the highlighted fields.', details: ['config.site_url: is required', 'kind: cannot be changed'] })
    expect(serverErrors(err, ['config.site_url'])).toEqual({ fields: { 'config.site_url': 'is required' }, general: ['kind: cannot be changed'] })
    expect(serverErrors(new Error('boom'), [])).toEqual({ fields: {}, general: ['boom'] })
  })

  it('errorKeys covers settings and secrets', () => {
    expect(errorKeys(HTTP_KIND)).toEqual(['name', 'kind', 'area_ids', 'secrets', 'config.base_url', 'config.auth_header', 'secrets.auth_value'])
  })
})
