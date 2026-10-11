import type { ReactNode } from 'react'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { Connection, ConnectionField } from '../../api/types.crewstate'
import { SelectField } from '../../components/Field'
import { folderLabel, isListed, splitIds, useKM2Folders, NOT_VISIBLE } from './km2Folders'

/** The most folders a KM2 connection may list besides its default (the server's limit) */
export const MAX_WRITE_FOLDERS = 50
export const PICK_LATER_HINT = 'Save the connection with its key, then pick folders from a list.'

type Props = {
  connection: Connection | null
  defaultField: ConnectionField
  writeField: ConnectionField
  config: Record<string, string>
  onChange: (key: string, value: string) => void
  errors: Record<string, string>
  /** Renders a setting as a text input (the fallback) */
  textField: (field: ConnectionField, hint?: string) => ReactNode
}

/**
 * A KM2 connection's save folders. Once the connection is saved with its key, the folders its key can see are picked
 * from lists; before that, or when they cannot be listed, the folder ids are typed, with the reason shown.
 */
export function KM2FolderFields({ connection, defaultField, writeField, config, onChange, errors, textField }: Props) {
  const keySaved = connection !== null && connection.secret_keys.includes('api_key')
  const state = useKM2Folders(keySaved ? connection.id : null, () => connectionsApi.km2Folders(connection!.id))
  if (state === null) {
    return <>{textField(defaultField, `${defaultField.hint ?? ''} ${PICK_LATER_HINT}`.trim())}{textField(writeField)}</>
  }
  if (state.status === 'failed') {
    return (
      <>
        <p className="text-[12px] text-warn" role="status">Could not list the folders, so type their ids: {state.message}</p>
        {textField(defaultField)}
        {textField(writeField)}
      </>
    )
  }
  if (state.status === 'loading') {
    return <SelectField label={defaultField.label} value="" onChange={() => undefined} disabled options={[{ value: '', label: 'Loading folders…' }]} />
  }
  const { list } = state
  const defaultID = config[defaultField.key] ?? ''
  const options = [{ value: '', label: 'None (saving off)' }, ...list.folders.map((f) => ({ value: f.id, label: f.path || f.name }))]
  if (defaultID && !isListed(list, defaultID)) options.push({ value: defaultID, label: folderLabel(list, defaultID) })
  const chosen = splitIds(config[writeField.key] ?? '')
  const unlisted = chosen.filter((id) => !isListed(list, id))
  const full = chosen.length >= MAX_WRITE_FOLDERS
  const toggle = (id: string, on: boolean) => onChange(writeField.key, (on ? [...chosen, id] : chosen.filter((x) => x !== id)).join(','))
  return (
    <>
      <SelectField label={defaultField.label} value={defaultID} onChange={(v) => onChange(defaultField.key, v)} options={options}
        error={errors[`config.${defaultField.key}`]} hint={defaultField.hint} />
      <fieldset className="flex flex-col gap-1">
        <legend className="mb-1 text-[12px] font-medium text-muted">{writeField.label} (optional)</legend>
        <div className="flex max-h-48 flex-col gap-1 overflow-y-auto rounded-md border border-line p-2" data-testid="write-folders">
          {[...list.folders.map((f) => ({ id: f.id, label: f.path || f.name })), ...unlisted.map((id) => ({ id, label: `${id} ${NOT_VISIBLE}` }))].map((f) => {
            const on = chosen.includes(f.id)
            return (
              <label key={f.id} className="flex items-center gap-2 text-[12.5px]">
                <input type="checkbox" checked={on} disabled={!on && full} onChange={(e) => toggle(f.id, e.target.checked)} />
                <span className="truncate">{f.label}</span>
              </label>
            )
          })}
          {list.folders.length === 0 && unlisted.length === 0 && <span className="text-[12px] text-faint">The key sees no folders.</span>}
        </div>
        {errors[`config.${writeField.key}`]
          ? <div className="text-[12px] text-danger" role="alert">{errors[`config.${writeField.key}`]}</div>
          : <div className="text-[11.5px] text-faint">{full ? `At most ${MAX_WRITE_FOLDERS} folders.` : 'Crews may name only these and the default folder.'}</div>}
      </fieldset>
    </>
  )
}
