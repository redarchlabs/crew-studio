import { useState, type FormEvent } from 'react'
import { TriangleAlert } from 'lucide-react'
import type { NewApiKeyInput } from '../../api/endpoints'
import type { NewApiKey } from '../../api/types'
import { Button } from '../../components/Button'
import { SelectField, TextField, type SelectOption } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { CopyButton } from '../../components/misc'
import { notifyError } from '../../lib/notify'
import { ApiKeyLimitsFields } from './ApiKeyLimitsFields'
import { limitsProblems, OPEN_LIMITS, toLimits, type KeyScope, type LimitsDraft } from './apiKeyLimits'
import { expiresAtFromDate, maskedKey, newKeyProblems } from './apiKeys'

/** A service account to pick in the form, with what it holds */
export type KeyAccountOption = SelectOption & { scope: KeyScope }

type NewKeyModalProps = {
  /** accountId is the chosen service account when `accounts` is given, else "" */
  create: (body: NewApiKeyInput, accountId: string) => Promise<NewApiKey>
  onClose: () => void
  onCreated: (created: NewApiKey) => void
  /** When given, the key's service account is picked in the form, and the key's limits are chosen within it */
  accounts?: readonly KeyAccountOption[]
  /** What the key's account holds, when it is not picked in the form */
  scope?: KeyScope
  who: 'you' | 'account'
}

type NewKeyErrors = { account?: string; name?: string; expires?: string; channels?: string; permissions?: string; areas?: string }

/** Asks for a key's name, expiry, limits and (optionally) account, and creates it. */
export function NewKeyModal({ create, onClose, onCreated, accounts, scope, who }: NewKeyModalProps) {
  const [accountId, setAccountId] = useState('')
  const [name, setName] = useState('')
  const [date, setDate] = useState('')
  const [limits, setLimits] = useState<LimitsDraft>(OPEN_LIMITS)
  const [errors, setErrors] = useState<NewKeyErrors>({})
  const [busy, setBusy] = useState(false)
  const keyScope = accounts ? accounts.find((a) => a.value === accountId)?.scope : scope

  const pickAccount = (id: string) => {
    setAccountId(id)
    // Choices made for another account may not fit this one
    setLimits(OPEN_LIMITS)
    setErrors(({ name: nameError, expires }) => ({ name: nameError, expires }))
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const problems: NewKeyErrors = {
      ...newKeyProblems(name, date),
      ...limitsProblems(limits),
      ...(accounts && !accountId ? { account: 'Choose the service account the key is for' } : {}),
    }
    setErrors(problems)
    if (Object.keys(problems).length > 0 || !keyScope) return
    setBusy(true)
    try {
      onCreated(await create({ name: name.trim(), expires_at: expiresAtFromDate(date), ...toLimits(limits, keyScope) }, accountId))
    } catch (err) {
      notifyError('Could not create the key', err)
      setBusy(false)
    }
  }

  return (
    <Modal
      title="New API key"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="new-key-form" busy={busy}>Create key</Button>
        </>
      }
    >
      <form id="new-key-form" className="flex flex-col gap-3" onSubmit={submit} noValidate>
        {accounts && (
          <SelectField
            label="Service account"
            value={accountId}
            onChange={pickAccount}
            options={[{ value: '', label: accounts.length ? 'Choose…' : 'No active service accounts' }, ...accounts]}
            error={errors.account}
            hint="The key acts as this account, with its roles and areas. People create their own keys under My API keys."
          />
        )}
        <TextField label="Name" value={name} onChange={setName} error={errors.name} placeholder="ERP integration" />
        <TextField label="Expires (optional)" type="date" value={date} onChange={setDate} error={errors.expires} hint="Leave empty for a key that does not expire. It stops working at the end of that day." />
        {keyScope && <ApiKeyLimitsFields value={limits} onChange={setLimits} scope={keyScope} who={who} errors={errors} />}
      </form>
    </Modal>
  )
}

/** Shows a new key's secret exactly once. */
export function SecretModal({ created, onClose }: { created: NewApiKey; onClose: () => void }) {
  return (
    <Modal title={`Key “${created.key.name}” created`} onClose={onClose} footer={<Button variant="primary" onClick={onClose}>I have saved it</Button>}>
      <div className="flex flex-col gap-3">
        <div className="flex items-start gap-2 rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px]" role="alert">
          <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warn" />
          <span>Copy this key now and store it somewhere safe. Crew Studio keeps only a one-way hash, so it cannot be shown again; if it is lost, revoke it and create another.</span>
        </div>
        <div className="flex items-center gap-2 rounded-md border border-line bg-raised p-2">
          <code className="min-w-0 flex-1 break-all font-mono text-[12px]">{created.secret}</code>
          <CopyButton text={created.secret} />
        </div>
        <p className="text-[12px] text-muted">
          Send it as <code className="font-mono">Authorization: Bearer {maskedKey(created.key.prefix)}</code> with each API request.
        </p>
      </div>
    </Modal>
  )
}
