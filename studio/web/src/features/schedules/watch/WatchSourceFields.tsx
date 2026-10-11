import { Link } from 'react-router'
import type { WatchConnection, WatchSourceKindInfo, WatchSourceKindKey } from '../../../api/types.watch'
import { NumberField, SelectField, TextField } from '../../../components/Field'
import { TrackerSourceFields } from './TrackerSourceFields'
import { fileFormat, hasItems, isTracker, type WatchForm } from './watchForm'

type Props = {
  form: WatchForm
  set: <K extends keyof WatchForm>(key: K, value: WatchForm[K]) => void
  kinds: WatchSourceKindInfo[]
  connections: WatchConnection[]
  crewId: string | null
  errors: Record<string, string>
}

const ITEM_HINTS: Record<WatchSourceKindKey, { id: string; tracked: string }> = {
  http_json: { id: 'Dotted path in each item, such as key or fields.id.', tracked: 'Comma-separated dotted paths, such as fields.status, fields.due_date.' },
  workspace_file: { id: 'A CSV column, or a dotted path in each JSON item.', tracked: 'Comma-separated CSV columns or dotted paths.' },
  web_page: { id: '', tracked: '' },
  tracker: { id: '', tracked: '' },
  tracking_list: { id: '', tracked: '' },
}

/** Source picker and the fields of the chosen source. */
export function WatchSourceFields({ form, set, kinds, connections, crewId, errors }: Props) {
  const kindOptions = kinds.map((k) => ({ value: k.kind, label: k.label }))
  const httpConnections = connections.filter((c) => c.kind === 'http')
  return (
    <div className="flex flex-col gap-3">
      <SelectField label="Source" value={form.kind} onChange={(v) => set('kind', v as WatchSourceKindKey)} options={kindOptions} />
      {form.kind === 'http_json' && (
        <>
          <SelectField
            label="Connection"
            value={form.connection_id}
            onChange={(v) => set('connection_id', v)}
            options={[{ value: '', label: httpConnections.length ? 'Choose a connection…' : 'No HTTP connections for this crew' }, ...httpConnections.map((c) => ({ value: c.id, label: c.name }))]}
            error={errors.connection_id}
            hint={<>The crew&apos;s enabled HTTP connections. {crewId && <Link className="text-accent" to={`/crews/${crewId}?tab=state`}>Choose them on the crew&apos;s State tab.</Link>}</>}
          />
          <TextField label="Path" value={form.path} onChange={(v) => set('path', v)} error={errors.path} placeholder="/issues?state=open" hint="Added to the connection's base URL; it cannot leave that host." />
          <TextField label="Items path" value={form.items_path} onChange={(v) => set('items_path', v)} placeholder="data.items" hint="Dotted path to the list of items in the response. Empty when the response is the list." />
        </>
      )}
      {form.kind === 'web_page' && (
        <>
          <TextField label="Page URL" value={form.url} onChange={(v) => set('url', v)} error={errors.url} placeholder="https://status.example.com" hint="Public pages only; private and local addresses are blocked, like the Scrape Website tool." />
          <TextField label="CSS selector" value={form.selector} onChange={(v) => set('selector', v)} placeholder="main .status" hint="Optional. Only the text of matching elements is compared." />
        </>
      )}
      {form.kind === 'workspace_file' && (
        <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_140px]">
          <TextField label="File" value={form.file} onChange={(v) => set('file', v)} error={errors.file} placeholder="watch/courses.csv" hint="A file in the Studio workspace's watch/ folder, where other systems drop files to watch." />
          <SelectField label="Format" value={form.format} onChange={(v) => set('format', v as WatchForm['format'])} options={[{ value: '', label: 'From extension' }, { value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }]} />
          {fileFormat(form) === 'json' && (
            <TextField className="sm:col-span-2" label="Items path (JSON)" value={form.items_path} onChange={(v) => set('items_path', v)} placeholder="items" hint="Dotted path to the list of items. Empty when the file is the list." />
          )}
        </div>
      )}
      {isTracker(form.kind) && <TrackerSourceFields form={form} set={set} connections={connections} crewId={crewId} errors={errors} />}
      {isTracker(form.kind) ? null : hasItems(form.kind) ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <TextField label="ID field" value={form.id_field} onChange={(v) => set('id_field', v)} error={errors.id_field} placeholder="key" hint={ITEM_HINTS[form.kind].id} />
          <TextField label="Tracked fields" value={form.tracked_fields} onChange={(v) => set('tracked_fields', v)} error={errors.tracked_fields} placeholder="status, due_date, assignee" hint={ITEM_HINTS[form.kind].tracked} />
          <NumberField label="Max items" value={form.max_items} onChange={(v) => set('max_items', v)} nullable min={1} max={10000} error={errors.max_items} hint="Default 2,000. More items fail the check instead of being cut." />
        </div>
      ) : (
        <p className="text-[12px] text-muted">The page&apos;s text is compared as one item. Long text is compared by a hash, so the change list says that it changed, not how.</p>
      )}
    </div>
  )
}
