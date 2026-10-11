import { useState } from 'react'
import { KeyRound } from 'lucide-react'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { Connection, ConnectionField, ConnectionKind } from '../../api/types.crewstate'
import { AreaPicker, defaultAreaIds } from '../../components/AreaChips'
import { Button } from '../../components/Button'
import { FormErrors, SelectField, TextField, Toggle } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { useAuth } from '../../lib/auth'
import {
  draftFromConnection,
  errorKeys,
  newDraft,
  secretFields,
  serverErrors,
  settingFields,
  toInput,
  validateDraft,
  type ConnectionDraft,
  type DraftErrors,
} from './connectionForm'
import { KM2FolderFields } from './KM2FolderFields'
import { clearFolderCache } from './km2Folders'

type Props = {
  kinds: ConnectionKind[]
  /** null creates a new connection */
  connection: Connection | null
  onClose: () => void
  onSaved: (c: Connection) => void
}

const INPUT_TYPES: Record<ConnectionField['input'], string> = {
  text: 'text', url: 'url', https_url: 'url', email: 'email', port: 'text', header: 'text', toggle: 'checkbox', cloud_id: 'text',
  uuid: 'text', uuid_list: 'text', select: 'text',
}

/** One input of a kind's settings or secrets. Secrets are password inputs that never show a saved value. */
export function KindField({ field, value, onChange, error }: { field: ConnectionField; value: string; onChange: (v: string) => void; error?: string }) {
  if (field.input === 'toggle') {
    return <Toggle label={field.label} checked={value === 'true'} onChange={(on) => onChange(on ? 'true' : '')} hint={error ?? field.hint} />
  }
  if (field.input === 'select') {
    const options = [{ value: '', label: field.required ? 'Choose…' : `Default${field.options?.[0] ? ` (${field.options[0]})` : ''}` },
      ...(field.options ?? []).map((o) => ({ value: o, label: o }))]
    return <SelectField label={field.label} value={value} onChange={onChange} options={options} error={error} hint={field.hint} />
  }
  return (
    <TextField
      label={`${field.label}${field.required ? '' : ' (optional)'}`}
      value={value}
      onChange={onChange}
      error={error}
      hint={field.hint}
      type={field.secret ? 'password' : INPUT_TYPES[field.input]}
      autoComplete={field.secret ? 'new-password' : 'off'}
      inputMode={field.input === 'port' ? 'numeric' : undefined}
    />
  )
}

/** Creates a connection (with its secrets) or changes one's name, settings, areas and enabled flag. */
export function ConnectionEditor({ kinds, connection, onClose, onSaved }: Props) {
  const { me } = useAuth()
  const creating = connection === null
  const [draft, setDraft] = useState<ConnectionDraft>(() => (connection ? draftFromConnection(connection) : newDraft(kinds[0].kind, defaultAreaIds(me))))
  const [errors, setErrors] = useState<DraftErrors>({})
  const [general, setGeneral] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const kind = kinds.find((k) => k.kind === draft.kind) ?? kinds[0]

  const set = <K extends keyof ConnectionDraft>(key: K, value: ConnectionDraft[K]) => setDraft((d) => ({ ...d, [key]: value }))
  const setIn = (group: 'config' | 'secrets', key: string, value: string) => setDraft((d) => ({ ...d, [group]: { ...d[group], [key]: value } }))

  const save = async () => {
    const found = validateDraft(draft, kind, creating)
    setErrors(found)
    setGeneral([])
    if (Object.keys(found).length > 0) return
    setSaving(true)
    try {
      const body = toInput(draft, kind, creating)
      const saved = connection ? await connectionsApi.update(connection.id, body) : await connectionsApi.create(body)
      // The folders a connection allows (or the address its key goes to) may have changed
      clearFolderCache()
      onSaved(saved)
    } catch (err) {
      const parsed = serverErrors(err, errorKeys(kind))
      setErrors(parsed.fields)
      setGeneral(parsed.general)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title={creating ? 'New connection' : `Edit ${connection.name}`}
      onClose={onClose}
      width="max-w-xl"
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" busy={saving} onClick={() => void save()}>{creating ? 'Create' : 'Save'}</Button></>}
    >
      <div className="flex flex-col gap-3">
        <TextField label="Name" value={draft.name} onChange={(v) => set('name', v)} error={errors.name} placeholder="Acme Jira" />
        {creating ? (
          <SelectField label="Kind" value={draft.kind} onChange={(v) => setDraft((d) => ({ ...d, kind: v as ConnectionDraft['kind'], config: {}, secrets: {} }))} options={kinds.map((k) => ({ value: k.kind, label: k.label }))} error={errors.kind} />
        ) : (
          <div className="flex flex-col gap-1 text-[12.5px] text-muted">
            <div>Kind: <span className="text-fg">{kind.label}</span> (cannot be changed)</div>
            {connection.secret_keys.length > 0 && (
              <div className="text-warn">Changing the address, host, port or auth header removes the saved secrets, so they never go somewhere new. Replace them afterwards.</div>
            )}
          </div>
        )}
        {settingFields(kind).map((f) => {
          const setting = (field: ConnectionField, hint?: string) => (
            <KindField key={field.key} field={hint ? { ...field, hint } : field} value={draft.config[field.key] ?? ''}
              onChange={(v) => setIn('config', field.key, v)} error={errors[`config.${field.key}`]} />
          )
          const writeField = kind.fields.find((x) => x.input === 'uuid_list')
          if (f.input === 'uuid_list' && kind.fields.some((x) => x.input === 'uuid')) return null
          if (f.input === 'uuid' && writeField) {
            return <KM2FolderFields key={f.key} connection={connection} defaultField={f} writeField={writeField} config={draft.config}
              onChange={(key, v) => setIn('config', key, v)} errors={errors} textField={setting} />
          }
          return setting(f)
        })}
        {creating && secretFields(kind).length > 0 && (
          <fieldset className="flex flex-col gap-3 rounded-md border border-line p-3">
            <legend className="flex items-center gap-1.5 px-1 text-[12px] font-medium text-muted"><KeyRound className="size-3.5" /> Secrets</legend>
            <p className="text-[12px] text-faint">Stored encrypted. Nobody can read them back, here or through the API; you can only replace them later.</p>
            {secretFields(kind).map((f) => (
              <KindField key={f.key} field={f} value={draft.secrets[f.key] ?? ''} onChange={(v) => setIn('secrets', f.key, v)} error={errors[`secrets.${f.key}`]} />
            ))}
          </fieldset>
        )}
        <AreaPicker value={draft.area_ids} onChange={(v) => set('area_ids', v)} error={errors.area_ids} />
        <Toggle label="Enabled" checked={draft.enabled} onChange={(v) => set('enabled', v)} hint="Runs only get enabled connections." />
        <FormErrors messages={general} />
      </div>
    </Modal>
  )
}
