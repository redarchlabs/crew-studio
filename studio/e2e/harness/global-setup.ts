import { randomBytes } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import {
  AUTH_DIR, STATE_FILE, BASE_URL, DB_NAME, FAKE_LLM_PORT, FAKE_LLM_URL, FAKE_TRACKER_PORT, FAKE_TRACKER_URL, FAKE_KM2_PORT, FAKE_KM2_URL, FAKE_DELIVERY_PORT, FAKE_DELIVERY_URL, FAKE_SMTP_PORT, FAKE_MCP_PORT, FAKE_MCP_URL, LOG_DIR, OUT_DIR, RUNTIME_DIR, SERVER_BIN, SERVER_PORT, STUDIO_DIR, WEB_DIST, E2E_DIR,
} from './paths.ts'
import { run, start, waitFor, killGroup } from './proc.ts'
import { startServer, waitHealthy, writeProviderEnv } from './server.ts'
import { psql } from './db.ts'
import { writeState } from './state.ts'

const skipBuild = process.env.E2E_SKIP_BUILD === '1'
const log = (msg: string) => console.log(`[e2e] ${msg}`)

function ensureServices(): void {
  log('ensuring Postgres and Azurite are up (docker compose)')
  run('docker', ['compose', '--env-file', '.env', 'up', '-d', '--wait'], { cwd: STUDIO_DIR, label: 'docker compose up' })
}

function resetDatabase(): void {
  log(`recreating database ${DB_NAME}`)
  psql('postgres', `DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE);\nCREATE DATABASE ${DB_NAME};`)
}

function build(): void {
  if (skipBuild && fs.existsSync(SERVER_BIN) && fs.existsSync(path.join(WEB_DIST, 'index.html'))) {
    log('E2E_SKIP_BUILD=1: reusing the previous server and web builds')
    return
  }
  log('building the Go server')
  fs.mkdirSync(path.dirname(SERVER_BIN), { recursive: true })
  // Built into e2e/out so the dev stack's server/bin and web/dist are never touched
  run('go', ['build', '-o', SERVER_BIN, './cmd/crewstudio'], {
    cwd: path.join(STUDIO_DIR, 'server'), env: { ...process.env, GOTOOLCHAIN: 'local' }, label: 'go build',
  })
  log('building the web app')
  const web = path.join(STUDIO_DIR, 'web')
  try {
    // `npm run build` is tsc -b && vite build; the extra flags go to vite and send the output away from web/dist
    run('npm', ['run', 'build', '--', '--outDir', WEB_DIST, '--emptyOutDir'], { cwd: web, label: 'web build' })
  } catch (err) {
    // A type error in a unit-test file someone is editing must not stop the browser suite: vite alone still bundles the app
    log(`npm run build failed (${String(err).split('\n').slice(1, 3).join(' ')}); retrying with vite build only`)
    run('npx', ['vite', 'build', '--outDir', WEB_DIST, '--emptyOutDir'], { cwd: web, label: 'vite build' })
  }
}

/** A crashed earlier run may have left its servers behind; stop those, and refuse to start on top of anything else. */
/** Only stop a recorded pid when it is still one of our processes (pids get reused). */
function ours(pid: number): boolean {
  try {
    return /crewstudio|fake_llm|fake_tracker|fake_km2|fake_delivery|fake_mcp/.test(fs.readFileSync(`/proc/${pid}/cmdline`, 'utf8'))
  } catch {
    return false
  }
}

async function stopLeftovers(): Promise<void> {
  if (fs.existsSync(STATE_FILE)) {
    try {
      const old = JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')) as { pids?: { server?: number; fakeLlm?: number; fakeTracker?: number; fakeKm2?: number; fakeDelivery?: number; fakeMcp?: number } }
      await Promise.all([old.pids?.server, old.pids?.fakeLlm, old.pids?.fakeTracker, old.pids?.fakeKm2, old.pids?.fakeDelivery, old.pids?.fakeMcp].filter((p): p is number => !!p).filter(ours).map((p) => killGroup(p)))
    } catch {
      // unreadable state: nothing to stop
    }
  }
  for (const port of [SERVER_PORT, FAKE_LLM_PORT, FAKE_TRACKER_PORT, FAKE_KM2_PORT, FAKE_DELIVERY_PORT, FAKE_MCP_PORT]) {
    const busy = await fetch(`http://127.0.0.1:${port}/`).then(() => true, () => false)
    if (busy) throw new Error(`port ${port} is already in use; stop whatever is listening there (the e2e suite needs it)`)
  }
}

export default async function globalSetup(): Promise<void> {
  const t0 = Date.now()
  await stopLeftovers()
  // Previous run state goes; bin/ and web-dist/ stay so E2E_SKIP_BUILD=1 can reuse them
  for (const dir of [LOG_DIR, AUTH_DIR, RUNTIME_DIR]) fs.rmSync(dir, { recursive: true, force: true })
  fs.mkdirSync(OUT_DIR, { recursive: true })
  fs.mkdirSync(LOG_DIR, { recursive: true })
  fs.mkdirSync(AUTH_DIR, { recursive: true })
  fs.mkdirSync(RUNTIME_DIR, { recursive: true })
  ensureServices()
  resetDatabase()
  build()

  const providerEnv = path.join(RUNTIME_DIR, 'provider.env')
  writeProviderEnv(providerEnv)
  fs.mkdirSync(path.join(RUNTIME_DIR, 'workspace'), { recursive: true })
  fs.mkdirSync(path.join(RUNTIME_DIR, 'data'), { recursive: true })

  const llmLog = path.join(LOG_DIR, 'fake-llm-requests.jsonl')
  const fakePid = start('python3', [path.join(E2E_DIR, 'harness', 'fake_llm.py'), '--port', String(FAKE_LLM_PORT), '--log', llmLog], {
    env: { PATH: process.env.PATH ?? '' }, logFile: path.join(LOG_DIR, 'fake-llm.log'),
  })
  await waitFor('the fake LLM server', async () => (await fetch(`${FAKE_LLM_URL}/__health`)).ok, 15_000)
  const trackerPid = start('python3', [path.join(E2E_DIR, 'harness', 'fake_tracker.py'), '--port', String(FAKE_TRACKER_PORT)], {
    env: { PATH: process.env.PATH ?? '' }, logFile: path.join(LOG_DIR, 'fake-tracker.log'),
  })
  await waitFor('the fake tracker server', async () => (await fetch(`${FAKE_TRACKER_URL}/__health`)).ok, 15_000)
  const km2Pid = start('python3', [path.join(E2E_DIR, 'harness', 'fake_km2.py'), '--port', String(FAKE_KM2_PORT)], {
    env: { PATH: process.env.PATH ?? '' }, logFile: path.join(LOG_DIR, 'fake-km2.log'),
  })
  await waitFor('the fake KM2 server', async () => (await fetch(`${FAKE_KM2_URL}/__health`)).ok, 15_000)
  const deliveryPid = start('python3', [path.join(E2E_DIR, 'harness', 'fake_delivery.py'), '--port', String(FAKE_DELIVERY_PORT), '--smtp-port', String(FAKE_SMTP_PORT)], {
    env: { PATH: process.env.PATH ?? '' }, logFile: path.join(LOG_DIR, 'fake-delivery.log'),
  })
  await waitFor('the fake delivery server', async () => (await fetch(`${FAKE_DELIVERY_URL}/__health`)).ok, 15_000)
  const mcpPid = start('python3', [path.join(E2E_DIR, 'harness', 'fake_mcp.py'), '--port', String(FAKE_MCP_PORT)], {
    env: { PATH: process.env.PATH ?? '' }, logFile: path.join(LOG_DIR, 'fake-mcp.log'),
  })
  await waitFor('the fake MCP server', async () => (await fetch(`${FAKE_MCP_URL}/__health`)).ok, 15_000)

  const serverLog = path.join(LOG_DIR, 'server.log')
  // The database is new each run, so a fresh connection secrets key is enough; workers inherit it for extra servers
  process.env.E2E_SECRETS_KEY = randomBytes(32).toString('base64')
  const serverPid = await startServer()
  // Persist pids immediately so teardown can clean up even if the rest of setup fails
  writeState({ baseURL: BASE_URL, fakeLlmURL: FAKE_LLM_URL, llmLog, serverLog, setupToken: '', dbName: DB_NAME, pids: { server: serverPid, fakeLlm: fakePid, fakeTracker: trackerPid, fakeKm2: km2Pid, fakeDelivery: deliveryPid, fakeMcp: mcpPid }, runtimeDir: RUNTIME_DIR })
  try {
    await waitHealthy(SERVER_PORT)
  } catch (err) {
    await Promise.all([killGroup(serverPid), killGroup(fakePid), killGroup(trackerPid), killGroup(km2Pid), killGroup(deliveryPid), killGroup(mcpPid)])
    const tail = fs.readFileSync(serverLog, 'utf8').split('\n').slice(-15).join('\n')
    throw new Error(`${String(err)}\n--- server log tail ---\n${tail}`)
  }

  const logText = fs.readFileSync(serverLog, 'utf8')
  const token = /\/setup\?token=([A-Za-z0-9_-]+)/.exec(logText)?.[1]
  if (!token) throw new Error('the server did not print a first-run setup token (is the database already set up?)')
  writeState({ baseURL: BASE_URL, fakeLlmURL: FAKE_LLM_URL, llmLog, serverLog, setupToken: token, dbName: DB_NAME, pids: { server: serverPid, fakeLlm: fakePid, fakeTracker: trackerPid, fakeKm2: km2Pid, fakeDelivery: deliveryPid, fakeMcp: mcpPid }, runtimeDir: RUNTIME_DIR })
  process.env.E2E_SETUP_TOKEN = token
  log(`ready in ${((Date.now() - t0) / 1000).toFixed(1)}s at ${BASE_URL} (logs in ${path.relative(E2E_DIR, LOG_DIR)})`)
}
