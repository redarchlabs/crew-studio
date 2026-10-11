import { Plus, Trash2 } from 'lucide-react'
import type { DestinationConnection, DestinationKind } from '../../api/types.delivery'
import { Button } from '../../components/Button'
import { SelectField, TextAreaField } from '../../components/Field'
import { connectionsApi } from '../../api/endpoints.crewstate'
import { KM2FolderSelect } from '../connections/KM2FolderSelect'
import { useKM2Folders } from '../connections/km2Folders'
import { connectionsOf, KIND_LABELS, MAX_DESTINATIONS, newRow, type DestinationRow } from './destinationsForm'

type Props = {
  rows: DestinationRow[]
  connections: DestinationConnection[]
  errors: Record<number, string>
  onChange: (rows: DestinationRow[]) => void
}

const KINDS = Object.keys(KIND_LABELS) as DestinationKind[]

/** A KM2 destination's folder, picked from the folders its connection allows once a connection is chosen. */
function DestinationFolder({ connectionId, value, onChange }: { connectionId: string; value: string; onChange: (v: string) => void }) {
  const state = useKM2Folders(connectionId || null, () => connectionsApi.km2Folders(connectionId))
  return <KM2FolderSelect state={state} value={value} onChange={onChange} what="reports" />
}

/** Edits a list of report destinations. The work order output is always included, so it is not a row. */
export function DestinationRows({ rows, connections, errors, onChange }: Props) {
  const update = (i: number, change: Partial<DestinationRow>) => onChange(rows.map((r, j) => (j === i ? { ...r, ...change } : r)))
  const setKind = (i: number, kind: DestinationKind) => update(i, { kind, connection_id: connectionsOf(connections, kind)[0]?.id ?? '' })
  return (
    <div className="flex flex-col gap-3">
      {rows.length === 0 && <p className="text-[12.5px] text-faint">Only the work order gets the report. Add a destination to also send it somewhere.</p>}
      {rows.map((r, i) => {
        const options = connectionsOf(connections, r.kind)
        return (
          <div key={i} className="flex flex-col gap-2 rounded-md border border-line bg-raised p-3" data-testid="destination-row">
            <div className="grid gap-2 sm:grid-cols-[10rem_1fr_auto] sm:items-end">
              <SelectField label="Destination" value={r.kind} onChange={(v) => setKind(i, v as DestinationKind)}
                options={KINDS.map((k) => ({ value: k, label: KIND_LABELS[k] }))} />
              <SelectField label="Connection" value={r.connection_id} onChange={(v) => update(i, { connection_id: v })}
                options={[{ value: '', label: options.length ? 'Choose…' : `No ${KIND_LABELS[r.kind]} connection the crew may use` },
                  ...options.map((c) => ({ value: c.id, label: c.name }))]} error={errors[i]} />
              <Button variant="ghost" size="sm" icon={<Trash2 className="size-3.5" />} onClick={() => onChange(rows.filter((_, j) => j !== i))} aria-label={`Remove destination ${i + 1}`}>Remove</Button>
            </div>
            {r.kind === 'email' && (
              <TextAreaField label="Recipients" rows={2} value={r.recipients} onChange={(v) => update(i, { recipients: v })}
                hint="Email addresses, separated by commas or new lines (at most 50)." />
            )}
            {r.kind === 'km2' && <DestinationFolder connectionId={r.connection_id} value={r.folder} onChange={(v) => update(i, { folder: v })} />}
          </div>
        )
      })}
      {rows.length < MAX_DESTINATIONS && (
        <div><Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => onChange([...rows, newRow(connections)])}>Add destination</Button></div>
      )}
    </div>
  )
}
