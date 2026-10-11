import { useState } from 'react'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { Connection, ConnectionKind } from '../../api/types.crewstate'
import { Button } from '../../components/Button'
import { FormErrors } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { errorKeys, secretFields, secretsToSend, serverErrors, validateSecrets, type DraftErrors } from './connectionForm'
import { KindField } from './ConnectionEditor'

type Props = { connection: Connection; kind: ConnectionKind; onClose: () => void; onSaved: (c: Connection) => void }

/** Replaces every secret of a connection. The old values are never shown; leaving a field empty removes it. */
export function SecretModal({ connection, kind, onClose, onSaved }: Props) {
  const [secrets, setSecrets] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<DraftErrors>({})
  const [general, setGeneral] = useState<string[]>([])
  const [saving, setSaving] = useState(false)

  const save = async () => {
    const found = validateSecrets(secrets, kind, true)
    setErrors(found)
    setGeneral(found.secrets ? [found.secrets] : [])
    if (Object.keys(found).length > 0) return
    setSaving(true)
    try {
      onSaved(await connectionsApi.replaceSecret(connection.id, secretsToSend(secrets, kind)))
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
      title={`Replace secret: ${connection.name}`}
      onClose={onClose}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" busy={saving} onClick={() => void save()}>Replace</Button></>}
    >
      <div className="flex flex-col gap-3">
        <p className="text-[12.5px] text-muted">
          The new values replace every saved secret of this connection. Saved values are never shown; a field left empty is removed.
        </p>
        {secretFields(kind).map((f) => (
          <KindField key={f.key} field={f} value={secrets[f.key] ?? ''} onChange={(v) => setSecrets((s) => ({ ...s, [f.key]: v }))} error={errors[`secrets.${f.key}`]} />
        ))}
        <FormErrors messages={general} />
      </div>
    </Modal>
  )
}
