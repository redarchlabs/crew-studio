import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { KeyRound, RefreshCw, UserRound } from 'lucide-react'
import { triggersApi } from '../../api/endpoints'
import type { Trigger, TriggerInput } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { EditorHeader } from '../../components/EditorHeader'
import { FormErrors, TextAreaField, TextField, Toggle } from '../../components/Field'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { ConfirmButton, CopyButton } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { dateTime, int, relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { BriefEditor } from '../workorders/composer/BriefEditor'
import { RecentWorkOrders } from '../workorders/RecentWorkOrders'
import { BudgetField } from '../workorders/target/BudgetField'
import { deriveTargetInputs, type TargetRef } from '../workorders/target/targetInputs'
import { TargetPicker } from '../workorders/target/TargetPicker'
import { loadTargetCatalog, type TargetCatalogData } from '../workorders/target/useTargetCatalog'
import { InputMapEditor } from './InputMapEditor'
import { SigningSecret } from './SigningSecret'
import { TokenModal } from './TokenModal'
import { emptyTrigger, normalizeTrigger, toTriggerInput, TRIGGER_FIELDS, validateTrigger } from './triggerForm'

type Revealed = { trigger: Trigger; token: string }

function TriggerEditor({ trigger, catalog }: { trigger: Trigger | null; catalog: TargetCatalogData }) {
  const navigate = useNavigate()
  const { can, me } = useAuth()
  const manage = can('triggers.manage')
  const [current, setCurrent] = useState<Trigger | null>(trigger)
  const [revealed, setRevealed] = useState<Revealed | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [rotating, setRotating] = useState(false)
  const initial = useMemo(() => (trigger ? toTriggerInput(trigger) : emptyTrigger()), [trigger])
  const form = useEntityForm<TriggerInput, Trigger>({
    entityId: current?.id ?? null,
    initial,
    fields: TRIGGER_FIELDS,
    label: 'trigger',
    validate: validateTrigger,
    normalize: normalizeTrigger,
    create: triggersApi.create,
    update: triggersApi.update,
  })
  useUnsavedGuard(form.dirty && manage && !revealed)
  const { value: v, set, errors } = form
  const f = errors.fields
  const specs = useMemo(() => deriveTargetInputs(v, catalog).inputs, [v, catalog])
  const crew = catalog.crews.find((c) => c.id === v.crew_id)

  const onSave = async () => {
    const saved = await form.save()
    if (!saved) return
    // Keep the secret we already had if the update response leaves it out
    setCurrent((prev) => ({ ...saved, signing_secret: saved.signing_secret ?? prev?.signing_secret }))
    if (!current && saved.token) setRevealed({ trigger: saved, token: saved.token })
  }
  const closeToken = () => {
    const created = !trigger && current
    setRevealed(null)
    if (created) navigate(`/triggers/${current.id}`, { replace: true, state: SKIP_GUARD })
  }
  const rotate = async () => {
    if (!current) return
    setRotating(true)
    try {
      const rotated = await triggersApi.rotate(current.id)
      setCurrent((prev) => ({ ...rotated, signing_secret: rotated.signing_secret ?? prev?.signing_secret }))
      if (rotated.token) setRevealed({ trigger: rotated, token: rotated.token })
      notifySuccess('Token rotated; the old token no longer works')
    } catch (err) {
      notifyError('Could not rotate the token', err)
    } finally {
      setRotating(false)
    }
  }
  const onDelete = async () => {
    if (!current) return
    setDeleting(true)
    try {
      await triggersApi.remove(current.id)
      notifySuccess(`Deleted ${current.name}`)
      navigate('/triggers', { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${current.name}`, err)
      setDeleting(false)
    }
  }

  const header = manage ? (
    <EditorHeader backTo="/triggers" backLabel="Triggers" title={current ? current.name : 'New trigger'} isNew={!current} dirty={form.dirty} saving={form.saving} entityType="trigger" entityId={current?.id} onSave={() => void onSave()} onDelete={() => void onDelete()} deleting={deleting} />
  ) : (
    <PageHeader eyebrow="Trigger" title={current?.name ?? 'Trigger'} description={<span>Read only: changing triggers needs <code className="font-mono text-[12px]">triggers.manage</code>.</span>} actions={<Link className="text-accent" to="/triggers">Back to triggers</Link>} />
  )

  return (
    <>
      {header}
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_380px]">
        <fieldset disabled={!manage} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
          <Panel title="Trigger">
            <div className="flex flex-col gap-3">
              <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder="New support ticket" />
              <TextAreaField label="Description" rows={2} value={v.description} onChange={(x) => set('description', x)} error={f.description} />
              <TargetPicker value={v} onChange={(t: TargetRef) => form.update((prev) => ({ ...prev, ...t }))} catalog={catalog} error={f.target ?? f.crew_id} />
              <Toggle label="Enabled" checked={v.enabled} onChange={(x) => set('enabled', x)} hint="A disabled trigger answers 409 and creates nothing." />
            </div>
          </Panel>
          <Panel title="Payload to inputs">
            <p className="mb-2 text-[12px] text-muted">Map each input to a dotted path in the JSON body, e.g. <code className="font-mono">customer.name</code> or <code className="font-mono">items.0.sku</code>.</p>
            <InputMapEditor key={current?.id ?? 'new'} value={v.input_map} onChange={(x) => set('input_map', x)} specs={specs} error={f.input_map} />
          </Panel>
          <Panel title="Brief">
            <div className="flex flex-col gap-3">
              <BriefEditor value={v.brief} onChange={(x) => set('brief', x)} error={f.brief} rows={6} hint="Placed before the payload in each work order's brief." />
              <Toggle label="Include the payload" checked={v.include_payload} onChange={(x) => set('include_payload', x)} hint="Append the JSON body to the brief so agents can read all of it." />
            </div>
          </Panel>
          <Panel title="Budget and callback">
            <div className="flex flex-col gap-3">
              <BudgetField value={v.budget_usd} onChange={(x) => set('budget_usd', x)} crewBudget={crew?.budget_usd} error={f.budget_usd} />
              <TextField label="Callback URL" value={v.callback_url ?? ''} onChange={(x) => set('callback_url', x)} error={f.callback_url} placeholder="https://example.com/hooks/crew-studio" hint="Optional. POSTed with the result when each run finishes; must be https and public unless the host is allow-listed." inputMode="url" />
            </div>
          </Panel>
          <FormErrors messages={errors.general} />
        </fieldset>
        <aside className="flex min-w-0 flex-col gap-4">
          {current ? <EndpointPanel trigger={current} canRotate={manage && can('work_orders.create')} rotating={rotating} onRotate={() => void rotate()} /> : <Panel title="Endpoint"><p className="text-[12px] text-muted">Create the trigger to get its URL and token. The token is shown once.</p></Panel>}
          {current && <Panel title="Callback signing secret"><SigningSecret secret={current.signing_secret} /></Panel>}
          <Panel title="Runs as">
            <div className="flex items-center gap-2 text-[13px]">
              <UserRound className="size-4 text-faint" />
              <span className="font-medium">{current?.run_as_name || me?.name || 'You'}</span>
              {current && <AreaChips areaIds={current.area_ids} max={2} />}
            </div>
            <p className="mt-1.5 text-[12px] text-muted">Work orders from this trigger run with the permissions and areas of the person who last saved it.{manage && current && current.run_as !== me?.id && <> Saving makes <span className="font-medium text-fg">you</span> the run-as person.</>}</p>
          </Panel>
        </aside>
      </div>
      {current && <div className="mt-5"><RecentWorkOrders source="webhook" sourceId={current.id} /></div>}
      {revealed && <TokenModal trigger={revealed.trigger} token={revealed.token} onClose={closeToken} />}
    </>
  )
}

function EndpointPanel({ trigger, canRotate, rotating, onRotate }: { trigger: Trigger; canRotate: boolean; rotating: boolean; onRotate: () => void }) {
  const url = triggersApi.hookUrl(trigger.id)
  return (
    <Panel title="Endpoint">
      <div className="flex flex-col gap-2.5 text-[12.5px]">
        <div>
          <div className="mb-1 flex items-center justify-between"><span className="eyebrow">POST</span><CopyButton text={url} /></div>
          <code className="block break-all rounded border border-line bg-raised px-2 py-1 font-mono text-[11.5px]">{url}</code>
        </div>
        <div className="flex items-center gap-2">
          <KeyRound className="size-3.5 text-faint" />
          <span className="font-mono text-[11.5px]">cst_{trigger.token_prefix}_…</span>
          {canRotate && (
            <span className="ml-auto"><ConfirmButton icon={<RefreshCw className="size-3.5" />} busy={rotating} onConfirm={onRotate}>Rotate token</ConfirmButton></span>
          )}
        </div>
        <div className="text-[12px] text-muted">
          Fired {int(trigger.fire_count)} times{trigger.last_fired_at && <>, last <span title={dateTime(trigger.last_fired_at)}>{relative(trigger.last_fired_at)}</span></>}.
        </div>
        <p className="text-[11.5px] text-faint">Rotating replaces the token at once; callers using the old one get 401.</p>
      </div>
    </Panel>
  )
}

export function TriggerEditorPage() {
  const { id = 'new' } = useParams()
  const res = useAsync(async (signal) => {
    const [catalog, trigger] = await Promise.all([loadTargetCatalog(signal), id === 'new' ? Promise.resolve(null) : triggersApi.get(id, signal)])
    return { catalog, trigger }
  }, [id])
  return (
    <AsyncView {...res}>
      {({ catalog, trigger }) =>
        id !== 'new' && !trigger ? <EmptyState title="Trigger not found" action={<Link className="text-accent" to="/triggers">Back to triggers</Link>} /> : <TriggerEditor key={id} trigger={trigger} catalog={catalog} />
      }
    </AsyncView>
  )
}
