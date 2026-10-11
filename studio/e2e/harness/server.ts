import fs from 'node:fs'
import path from 'node:path'
import {
  AZURE_CONTAINER, BASE_URL, DB_NAME, FAKE_DELIVERY_URL, FAKE_LLM_PORT, FAKE_LLM_URL, FAKE_TRACKER_URL, LOG_DIR, PYTHON, RUNTIME_DIR, SERVER_BIN, SERVER_HOST, SERVER_PORT, STUDIO_DIR, WEB_DIST,
} from './paths.ts'
import { start, waitFor } from './proc.ts'
import { dbSettings } from './secrets.ts'

/** Only fake credentials ever reach the runner: it reads this file and inherits nothing else from the provider. */
export function writeProviderEnv(file: string): void {
  const body = [
    'OPENAI_API_KEY=sk-fake-e2e-0000000000000000',
    'GEMINI_API_KEY=fake',
    'ANTHROPIC_API_KEY=fake',
    `OPENAI_BASE_URL=${FAKE_LLM_URL}/v1`,
    `ANTHROPIC_BASE_URL=${FAKE_LLM_URL}`,
    '',
  ].join('\n')
  fs.writeFileSync(file, body, { mode: 0o600 })
}

/**
 * The server's whole environment. A strict allow-list: nothing from the developer's shell (API keys, cloud
 * credentials) reaches it, and the studio dir passed on the command line holds no .env.
 */
export function serverEnv(port = SERVER_PORT, overrides: Record<string, string> = {}): NodeJS.ProcessEnv {
  const { password, port: dbPort } = dbSettings()
  const publicURL = `http://${SERVER_HOST}:${port}`
  const env: NodeJS.ProcessEnv = {}
  for (const key of ['PATH', 'HOME', 'LANG', 'USER', 'TMPDIR']) if (process.env[key]) env[key] = process.env[key]
  Object.assign(env, {
    STUDIO_ADDR: `${SERVER_HOST}:${port}`,
    STUDIO_DATABASE_URL: `postgres://crewstudio:${encodeURIComponent(password)}@127.0.0.1:${dbPort}/${DB_NAME}?sslmode=disable`,
    STUDIO_WEB_DIR: WEB_DIST,
    STUDIO_PUBLIC_URL: publicURL,
    STUDIO_WORKSPACE: path.join(RUNTIME_DIR, 'workspace'),
    STUDIO_DATA_DIR: path.join(RUNTIME_DIR, 'data'),
    STUDIO_PROVIDER_ENV_FILE: path.join(RUNTIME_DIR, 'provider.env'),
    STUDIO_PYTHON: PYTHON,
    STUDIO_RUNNER: path.join(STUDIO_DIR, 'runner', 'run_crew.py'),
    STUDIO_STORAGE: 'azure',
    STUDIO_AZURE_STORAGE_CONNECTION_STRING: 'UseDevelopmentStorage=true',
    STUDIO_AZURE_CONTAINER: AZURE_CONTAINER,
    STUDIO_LOCAL_LOGIN: 'true',
    STUDIO_ALLOWED_ORIGINS: publicURL,
    STUDIO_LOG_LEVEL: 'info',
    // Help search would call Gemini embeddings with the fake key; keyword search stays offline
    STUDIO_HELP_SEARCH: 'keyword',
    // Crew memory embeds with OpenAI, which the provider env points at the fake's deterministic /v1/embeddings
    // (OPENAI_API_KEY and OPENAI_BASE_URL come from writeProviderEnv); help search above stays keyword
    STUDIO_MEMORY_EMBEDDINGS: 'openai',
    // A throwaway key made by global-setup for this run's database, so connections can be saved
    STUDIO_SECRETS_KEY: process.env.E2E_SECRETS_KEY ?? '',
    // Tracker connections may reach only the fake tracker besides real Atlassian sites
    STUDIO_JIRA_SITES: FAKE_TRACKER_URL,
    STUDIO_LINEAR_API_URL: `${FAKE_TRACKER_URL}/graphql`,
    // Report webhooks may reach only the fake Slack and Teams besides real https hosts
    STUDIO_DELIVERY_ORIGINS: FAKE_DELIVERY_URL,
    STUDIO_SLACK_API_URL: `${FAKE_DELIVERY_URL}/slack/api`,
    CREWAI_DISABLE_TELEMETRY: 'true',
    OTEL_SDK_DISABLED: 'true',
    ...overrides,
  })
  return env
}

/** Starts a server process (the harness's main one, or an extra one for a test) and waits until it is healthy. */
export async function startServer(opts: { port?: number; overrides?: Record<string, string>; logName?: string } = {}): Promise<number> {
  const port = opts.port ?? SERVER_PORT
  const studioDir = path.join(RUNTIME_DIR, 'studio')
  fs.mkdirSync(studioDir, { recursive: true })
  const pid = start(SERVER_BIN, ['-studio-dir', studioDir], {
    env: serverEnv(port, opts.overrides),
    logFile: path.join(LOG_DIR, opts.logName ?? 'server.log'),
  })
  return pid
}

export async function waitHealthy(port: number, timeoutMs = 90_000): Promise<void> {
  await waitFor(`the server on :${port}`, async () => (await fetch(`http://${SERVER_HOST}:${port}/healthz`)).ok, timeoutMs)
}

export { BASE_URL, FAKE_LLM_PORT }
