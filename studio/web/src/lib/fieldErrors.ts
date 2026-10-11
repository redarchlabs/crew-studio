import { ApiError } from '../api/client'

export type FieldErrors = { fields: Record<string, string>; general: string[] }

export const NO_ERRORS: FieldErrors = { fields: {}, general: [] }

/**
 * Turns 422 `details` ("field: problem") into per-field messages.
 * Details whose field is not in `known` are returned as general messages.
 */
export function parseFieldErrors(err: unknown, known: readonly string[]): FieldErrors {
  if (!(err instanceof ApiError) || err.details.length === 0) return NO_ERRORS
  const fields: Record<string, string> = {}
  const general: string[] = []
  for (const detail of err.details) {
    const idx = detail.indexOf(':')
    const rawField = idx > 0 ? detail.slice(0, idx).trim() : ''
    const message = idx > 0 ? detail.slice(idx + 1).trim() : detail
    // "inputs[2].key" -> "inputs"; "tools.0" -> "tools"
    const root = rawField.split(/[.[]/)[0]
    if (root && known.includes(root)) {
      fields[root] = fields[root] ? `${fields[root]}; ${message}` : message
    } else {
      general.push(detail)
    }
  }
  return { fields, general }
}
