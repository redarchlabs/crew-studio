import type { DestinationConnection, DestinationKind, ReportDestination, ReportDestinationInput } from '../../api/types.delivery'

/** One destination row while someone edits: recipients are typed as text. */
export type DestinationRow = { kind: DestinationKind; connection_id: string; recipients: string; folder: string }

export const MAX_DESTINATIONS = 10
export const MAX_RECIPIENTS = 50

export const KIND_LABELS: Record<DestinationKind, string> = { slack: 'Slack', teams: 'Microsoft Teams', email: 'Email', km2: 'KM2' }

const EMAIL = /^[^\s@<>,;"()[\]\\]+@[^\s@<>,;"()[\]\\]+$/
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function toRows(dests: readonly ReportDestination[]): DestinationRow[] {
  return dests.map((d) => ({ kind: d.kind, connection_id: d.connection_id, recipients: d.recipients.join(', '), folder: d.folder }))
}

/** Recipients typed with commas, semicolons, spaces or new lines between them. */
export function splitRecipients(text: string): string[] {
  return text.split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean)
}

export function toInputs(rows: readonly DestinationRow[]): ReportDestinationInput[] {
  return rows.map((r) => ({
    kind: r.kind,
    connection_id: r.connection_id,
    recipients: r.kind === 'email' ? splitRecipients(r.recipients) : [],
    folder: r.kind === 'km2' ? r.folder.trim() : '',
  }))
}

/** The connections of a kind, for a row's picker. */
export function connectionsOf(conns: readonly DestinationConnection[], kind: DestinationKind): DestinationConnection[] {
  return conns.filter((c) => c.kind === kind)
}

/** A new row for the first kind that has a connection (or Slack). */
export function newRow(conns: readonly DestinationConnection[]): DestinationRow {
  const first = conns.find((c) => c.kind in KIND_LABELS)
  const kind = (first?.kind as DestinationKind | undefined) ?? 'slack'
  return { kind, connection_id: first?.id ?? '', recipients: '', folder: '' }
}

/** Problems keyed by row index, mirroring the server's checks. */
export function validateRows(rows: readonly DestinationRow[]): Record<number, string> {
  const errors: Record<number, string> = {}
  const seen = new Set<string>()
  rows.forEach((r, i) => {
    const recipients = splitRecipients(r.recipients)
    const key = JSON.stringify([r.kind, r.connection_id, r.kind === 'email' ? recipients : [], r.kind === 'km2' ? r.folder.trim().toLowerCase() : ''])
    if (!r.connection_id) errors[i] = 'Choose a connection'
    else if (r.kind === 'email' && recipients.length === 0) errors[i] = 'List at least one email address'
    else if (r.kind === 'email' && recipients.length > MAX_RECIPIENTS) errors[i] = `At most ${MAX_RECIPIENTS} addresses`
    else if (r.kind === 'email' && recipients.some((a) => !EMAIL.test(a) || !a.split('@')[1].includes('.'))) {
      errors[i] = `Not a plain email address: ${recipients.find((a) => !EMAIL.test(a) || !a.split('@')[1].includes('.'))}`
    } else if (r.kind === 'km2' && r.folder.trim() && !UUID.test(r.folder.trim())) errors[i] = 'The folder must be a KM2 folder id (a UUID), or empty for the default'
    else if (seen.has(key)) errors[i] = 'Same as an earlier destination'
    seen.add(key)
  })
  return errors
}

/** A short description of a saved destination. */
export function describeDestination(d: ReportDestination): string {
  const base = `${KIND_LABELS[d.kind]}: ${d.connection_name}`
  if (d.kind === 'email') return `${base} → ${d.recipients.join(', ')}`
  if (d.kind === 'km2') return `${base}${d.folder ? ` (folder ${d.folder})` : ' (default folder)'}`
  return base
}
