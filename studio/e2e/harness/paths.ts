import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))

export const E2E_DIR = path.resolve(here, '..')
export const STUDIO_DIR = path.resolve(E2E_DIR, '..')
export const REPO_DIR = path.resolve(STUDIO_DIR, '..')
// E2E_OUT_DIR, E2E_PORT, E2E_FAKE_LLM_PORT and E2E_DB_NAME let a second run (another checkout, or the help
// screenshot spec) work next to a suite that is already running, without either wiping the other's database
const env = (key: string, fallback: string): string => process.env[key] || fallback
export const OUT_DIR = path.resolve(E2E_DIR, env('E2E_OUT_DIR', 'out'))
export const RUNTIME_DIR = path.join(OUT_DIR, 'runtime')
export const AUTH_DIR = path.join(OUT_DIR, 'auth')
export const LOG_DIR = path.join(OUT_DIR, 'logs')
export const STATE_FILE = path.join(OUT_DIR, 'state.json')

export const SERVER_BIN = path.join(OUT_DIR, 'bin', 'crewstudio')
export const WEB_DIST = path.join(OUT_DIR, 'web-dist')

export const SERVER_HOST = '127.0.0.1'
export const SERVER_PORT = Number(env('E2E_PORT', '8091'))
export const FAKE_LLM_PORT = Number(env('E2E_FAKE_LLM_PORT', '8092'))
export const BASE_URL = `http://${SERVER_HOST}:${SERVER_PORT}`
export const FAKE_LLM_URL = `http://127.0.0.1:${FAKE_LLM_PORT}`
// The fake Jira (and Linear) the tracker specs use; E2E_FAKE_TRACKER_PORT moves it for a second suite
export const FAKE_TRACKER_PORT = Number(env('E2E_FAKE_TRACKER_PORT', '8094'))
export const FAKE_TRACKER_URL = `http://127.0.0.1:${FAKE_TRACKER_PORT}`
// The fake KM2 the KM2 spec uses (connections point at it); E2E_FAKE_KM2_PORT moves it for a second suite
export const FAKE_KM2_PORT = Number(env('E2E_FAKE_KM2_PORT', '8095'))
export const FAKE_KM2_URL = `http://127.0.0.1:${FAKE_KM2_PORT}`
// The fake Slack, Teams (HTTP) and SMTP server the PMO spec delivers reports to; E2E_FAKE_DELIVERY_PORT and
// E2E_FAKE_SMTP_PORT move them for a second suite
export const FAKE_DELIVERY_PORT = Number(env('E2E_FAKE_DELIVERY_PORT', '8096'))
export const FAKE_DELIVERY_URL = `http://127.0.0.1:${FAKE_DELIVERY_PORT}`
export const FAKE_SMTP_PORT = Number(env('E2E_FAKE_SMTP_PORT', '8097'))
// The fake outside MCP server the MCP connections spec points at (fake_mcp.py); E2E_FAKE_MCP_PORT moves it
export const FAKE_MCP_PORT = Number(env('E2E_FAKE_MCP_PORT', '8098'))
export const FAKE_MCP_URL = `http://127.0.0.1:${FAKE_MCP_PORT}`

export const DB_NAME = env('E2E_DB_NAME', 'crewstudio_e2e')
export const PG_CONTAINER = 'crewstudio-postgres-1'
export const PG_USER = 'crewstudio'
export const AZURE_CONTAINER = DB_NAME.replaceAll('_', '-')
export const PYTHON = path.join(REPO_DIR, '.venv', 'bin', 'python')
