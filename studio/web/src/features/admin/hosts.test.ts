import { describe, expect, it } from 'vitest'
import { hostProblem, normalizeHost } from './hosts'

describe('normalizeHost', () => {
  it('trims, lowercases and drops a trailing dot', () => {
    expect(normalizeHost('  ERP.Internal. ')).toBe('erp.internal')
  })
})

describe('hostProblem', () => {
  it('accepts host names, single labels and IP addresses', () => {
    for (const host of ['erp.internal', 'localhost', 'my-host.corp.example', '10.0.0.5', '192.168.1.255', 'fd00::1', '2001:db8:0:0:0:0:0:1']) {
      expect(hostProblem(host, []), host).toBeUndefined()
    }
  })

  it('rejects URLs, paths, ports and junk with a reason', () => {
    expect(hostProblem('https://erp.internal', [])).toMatch(/without http/)
    expect(hostProblem('erp.internal/hooks', [])).toMatch(/path/)
    expect(hostProblem('erp.internal:8080', [])).toMatch(/port/)
    expect(hostProblem('[fd00::1]', [])).toMatch(/brackets/)
    expect(hostProblem('bad host', [])).toMatch(/spaces/)
    expect(hostProblem('-bad.example', [])).toMatch(/Not a valid/)
    expect(hostProblem('300.1.1.1', [])).toMatch(/Not a valid/)
    expect(hostProblem('', [])).toMatch(/Enter/)
  })

  it('rejects duplicates', () => {
    expect(hostProblem('erp.internal', ['erp.internal'])).toMatch(/Already/)
  })
})
