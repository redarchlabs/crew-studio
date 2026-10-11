import fs from 'node:fs'
import path from 'node:path'
import { STUDIO_DIR } from './paths.ts'

/**
 * Reads only STUDIO_DB_PASSWORD and STUDIO_DB_PORT, from the environment first and studio/.env second
 * (parsed in memory, equivalent to sourcing it). The values are never printed, logged or written to a file.
 */
export function dbSettings(): { password: string; port: string } {
  const fromFile: Record<string, string> = {}
  const envPath = path.join(STUDIO_DIR, '.env')
  if (fs.existsSync(envPath)) {
    for (const raw of fs.readFileSync(envPath, 'utf8').split('\n')) {
      const line = raw.trim().replace(/^export\s+/, '')
      if (!line || line.startsWith('#')) continue
      const i = line.indexOf('=')
      if (i < 0) continue
      const key = line.slice(0, i).trim()
      if (key === 'STUDIO_DB_PASSWORD' || key === 'STUDIO_DB_PORT') fromFile[key] = line.slice(i + 1).trim().replace(/^(['"])(.*)\1$/, '$2')
    }
  }
  const password = process.env.STUDIO_DB_PASSWORD || fromFile.STUDIO_DB_PASSWORD
  const port = process.env.STUDIO_DB_PORT || fromFile.STUDIO_DB_PORT || '55432'
  if (!password) throw new Error('STUDIO_DB_PASSWORD is not set (source studio/.env or export it)')
  return { password, port }
}
