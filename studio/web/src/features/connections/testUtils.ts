import type { Connection, ConnectionKind, ConnectionKinds } from '../../api/types.crewstate'

// Shared fixtures for the connections and crew state tests (mirrors the server's kinds for jira, http and slack).

export const JIRA_KIND: ConnectionKind = {
  kind: 'jira',
  label: 'Jira Cloud',
  testable: false,
  fields: [
    { key: 'site_url', label: 'Site URL', input: 'url', secret: false, required: true, hint: 'Such as https://acme.atlassian.net' },
    { key: 'email', label: 'Account email', input: 'email', secret: false, required: true },
    { key: 'api_token', label: 'API token', input: 'text', secret: true, required: true },
  ],
}

export const HTTP_KIND: ConnectionKind = {
  kind: 'http',
  label: 'HTTP (JSON API)',
  testable: true,
  fields: [
    { key: 'base_url', label: 'Base URL', input: 'url', secret: false, required: true },
    { key: 'auth_header', label: 'Auth header name', input: 'header', secret: false, required: false },
    { key: 'auth_value', label: 'Auth header value', input: 'text', secret: true, required: false },
  ],
}

export const SLACK_KIND: ConnectionKind = {
  kind: 'slack',
  label: 'Slack',
  testable: false,
  any_secret: ['webhook_url', 'bot_token'],
  fields: [
    { key: 'channel', label: 'Channel', input: 'text', secret: false, required: false },
    { key: 'webhook_url', label: 'Incoming webhook URL', input: 'https_url', secret: true, required: false },
    { key: 'bot_token', label: 'Bot token', input: 'text', secret: true, required: false },
  ],
}

export const KINDS: ConnectionKinds = { kinds: [JIRA_KIND, HTTP_KIND, SLACK_KIND], secrets_ready: true }

export function makeConnection(over: Partial<Connection> = {}): Connection {
  const ts = '2026-01-01T00:00:00Z'
  return {
    id: 'conn-1',
    name: 'Ticket API',
    kind: 'http',
    config: { base_url: 'https://api.example' },
    secret_keys: [],
    missing_secrets: [],
    secret_updated_at: null,
    area_ids: ['area-1'],
    enabled: true,
    crew_count: 0,
    created_by: null,
    created_at: ts,
    updated_at: ts,
    ...over,
  }
}
