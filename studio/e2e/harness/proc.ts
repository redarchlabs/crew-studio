import { spawn, spawnSync, type SpawnSyncReturns } from 'node:child_process'
import fs from 'node:fs'

/** Run a command to completion; throws with the (secret-free) tail of its output on failure. */
export function run(cmd: string, args: string[], opts: { cwd?: string; env?: NodeJS.ProcessEnv; label?: string; input?: string } = {}): SpawnSyncReturns<string> {
  const res = spawnSync(cmd, args, { cwd: opts.cwd, env: opts.env ?? process.env, encoding: 'utf8', input: opts.input, maxBuffer: 64 << 20 })
  if (res.status !== 0) {
    const tail = `${res.stdout ?? ''}\n${res.stderr ?? ''}`.trim().split('\n').slice(-25).join('\n')
    throw new Error(`${opts.label ?? cmd} failed (exit ${res.status})\n${tail}`)
  }
  return res
}

/** Start a detached process whose stdout and stderr go to logFile; returns its pid (also its process group id). */
export function start(cmd: string, args: string[], opts: { cwd?: string; env: NodeJS.ProcessEnv; logFile: string }): number {
  const fd = fs.openSync(opts.logFile, 'w')
  const child = spawn(cmd, args, { cwd: opts.cwd, env: opts.env, detached: true, stdio: ['ignore', fd, fd] })
  child.unref()
  fs.closeSync(fd)
  if (!child.pid) throw new Error(`could not start ${cmd}`)
  return child.pid
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0)
  } catch {
    return false
  }
  try {
    // A killed child that has not been reaped yet shows as a zombie
    return !/^\d+ \(.*\) Z/.test(fs.readFileSync(`/proc/${pid}/stat`, 'utf8'))
  } catch {
    return true
  }
}

/** Stops a process group: SIGTERM, then SIGKILL if it is still around after a few seconds. */
export async function killGroup(pid: number): Promise<void> {
  for (const signal of ['SIGTERM', 'SIGKILL'] as const) {
    try {
      process.kill(-pid, signal)
    } catch {
      return // already gone
    }
    const until = Date.now() + 6000
    while (Date.now() < until) {
      if (!alive(pid)) return
      await new Promise((r) => setTimeout(r, 100))
    }
  }
}

export async function waitFor(what: string, probe: () => Promise<boolean> | boolean, timeoutMs = 60_000): Promise<void> {
  const until = Date.now() + timeoutMs
  let lastError: unknown
  while (Date.now() < until) {
    try {
      if (await probe()) return
    } catch (err) {
      lastError = err
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`timed out waiting for ${what}${lastError ? `: ${String(lastError)}` : ''}`)
}
