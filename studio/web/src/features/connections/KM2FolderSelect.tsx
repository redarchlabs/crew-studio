import { TriangleAlert } from 'lucide-react'
import { SelectField, TextField, type SelectOption } from '../../components/Field'
import { folderLabel, isListed, type FolderState } from './km2Folders'

type Props = {
  /** The connection's folder list; null when no connection is chosen yet */
  state: FolderState | null
  value: string
  onChange: (value: string) => void
  label?: string
  error?: string
  /** What the folder is for, in the warning when the connection has no default folder ("reports", "the document") */
  what?: string
}

export const NO_DEFAULT = 'No default folder: saving is off'

/**
 * Picks a folder a KM2 connection lets crews save to: its default folder (the empty value) or one of its other
 * folders, by path. Falls back to typing a folder id when the folders cannot be listed.
 */
export function KM2FolderSelect({ state, value, onChange, label = 'Folder', error, what = 'it' }: Props) {
  if (state === null || state.status === 'failed') {
    const why = state ? `Could not list the folders: ${state.message} ` : ''
    return <TextField label={`${label} (optional)`} value={value} onChange={onChange} error={error}
      hint={`${why}A KM2 folder id the connection allows; leave empty for its default folder.`} />
  }
  if (state.status === 'loading') {
    return <SelectField label={label} value="" onChange={() => undefined} disabled options={[{ value: '', label: 'Loading folders…' }]} />
  }
  const { list } = state
  const def = list.allowed.default_folder_id
  const options: SelectOption[] = [{ value: '', label: def ? `Connection's default folder (${folderLabel(list, def)})` : NO_DEFAULT }]
  for (const id of list.allowed.write_folder_ids) {
    if (id !== def) options.push({ value: id, label: folderLabel(list, id) })
  }
  if (value && !options.some((o) => o.value === value)) {
    options.push({ value, label: isListed(list, value) ? `${folderLabel(list, value)} (not allowed by the connection)` : folderLabel(list, value) })
  }
  return (
    <div className="flex flex-col gap-1">
      <SelectField label={label} value={value} onChange={onChange} options={options} error={error}
        hint={def || value ? 'The folders this connection lets crews save to.' : undefined} />
      {!def && !value && (
        <p className="flex items-start gap-1.5 text-[12px] text-warn" role="status">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
          This connection has no default folder, so {what} cannot be filed. Pick a folder, or set a default folder on the connection.
        </p>
      )}
    </div>
  )
}
