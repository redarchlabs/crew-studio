import { PG_CONTAINER, PG_USER } from './paths.ts'
import { run } from './proc.ts'

/** Runs SQL against a database inside the Postgres container (no password: local socket) and returns raw tuples. */
export function psql(db: string, sql: string): string {
  const res = run('docker', ['exec', '-i', PG_CONTAINER, 'psql', '-U', PG_USER, '-d', db, '-v', 'ON_ERROR_STOP=1', '-tA', '-F', '|'], {
    input: sql,
    label: 'psql',
  })
  return res.stdout.trim()
}
