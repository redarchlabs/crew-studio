import { useId, type ReactNode } from 'react'
import { adminApi, areasApi } from '../../api/endpoints'
import type { ApiKeyLimits } from '../../api/types'
import { Button } from '../../components/Button'
import { Chip, type Tone } from '../../components/StatusBadge'
import { useAsync } from '../../lib/useAsync'
import { PermissionMatrix } from './PermissionMatrix'
import { accessSummary, limitableAreas, readOnly, type KeyScope, type LimitsDraft, type LimitsProblems } from './apiKeyLimits'

type CheckProps = { label: string; checked: boolean; onChange: (on: boolean) => void; hint?: string; invalid?: boolean }

function Check({ label, checked, onChange, hint, invalid }: CheckProps) {
  const hintId = useId()
  return (
    <span className="inline-flex items-center gap-1.5 text-[12.5px]">
      <label className="inline-flex items-center gap-1.5">
        <input
          type="checkbox"
          className="size-3.5 accent-[var(--accent)]"
          checked={checked}
          aria-invalid={invalid || undefined}
          aria-describedby={hint ? hintId : undefined}
          onChange={(e) => onChange(e.target.checked)}
        />
        <span>{label}</span>
      </label>
      {hint && <span id={hintId} className="text-[11.5px] text-faint">{hint}</span>}
    </span>
  )
}

function FieldError({ id, text }: { id: string; text?: string }) {
  return text ? <p id={id} className="text-[12px] text-danger" role="alert">{text}</p> : null
}

type LimitChoiceProps = {
  legend: string
  limited: boolean
  onLimited: (on: boolean) => void
  all: string
  some: string
  /** Why "only these" cannot be chosen, when it cannot */
  someDisabled?: string
  error?: string
  children: ReactNode
}

/** "Everything" or "only these", with the choices shown when limited. */
function LimitChoice({ legend, limited, onLimited, all, some, someDisabled, error, children }: LimitChoiceProps) {
  const name = useId()
  const errorId = `${name}-error`
  return (
    <fieldset className="flex flex-col gap-1.5" aria-describedby={error ? errorId : undefined}>
      <legend className="mb-1 font-medium">{legend}</legend>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-[12.5px]">
        <label className="inline-flex items-center gap-1.5">
          <input type="radio" name={name} className="accent-[var(--accent)]" checked={!limited} onChange={() => onLimited(false)} />
          {all}
        </label>
        <label className="inline-flex items-center gap-1.5">
          <input type="radio" name={name} className="accent-[var(--accent)]" checked={limited} disabled={!!someDisabled} onChange={() => onLimited(true)} />
          {some}
        </label>
        {someDisabled && <span className="text-[11.5px] text-faint">{someDisabled}</span>}
      </div>
      {limited && children}
      <FieldError id={errorId} text={error} />
    </fieldset>
  )
}

type ApiKeyLimitsFieldsProps = {
  value: LimitsDraft
  onChange: (next: LimitsDraft) => void
  /** What the key's account holds; nothing outside it is offered */
  scope: KeyScope
  /** Whose key it is: your own, or a service account's */
  who: 'you' | 'account'
  errors: LimitsProblems
}

const WORDS = {
  you: { everything: 'Everything you can do', everyArea: 'Every area you are in', nothing: 'You hold no permissions to choose from.' },
  account: { everything: 'Everything this account can do', everyArea: 'Every area it is in', nothing: 'This account holds no permissions to choose from.' },
}

/** Where a new key works, and which of its account's permissions and areas it keeps. */
export function ApiKeyLimitsFields({ value, onChange, scope, who, errors }: ApiKeyLimitsFieldsProps) {
  const defs = useAsync((signal) => adminApi.permissions(signal), [])
  const areas = useAsync((signal) => areasApi.list(signal), [])
  const channelsError = useId()
  const set = <K extends keyof LimitsDraft>(key: K, v: LimitsDraft[K]) => onChange({ ...value, [key]: v })
  const heldDefs = (defs.data ?? []).filter((d) => scope.permissions.includes(d.key))
  const offeredAreas = limitableAreas(areas.data ?? [], scope)
  const words = WORDS[who]

  return (
    <div className="flex flex-col gap-3 rounded-md border border-line px-3 py-2.5">
      <fieldset className="flex flex-col gap-1.5" aria-describedby={errors.channels ? channelsError : undefined}>
        <legend className="mb-1 font-medium">Works with</legend>
        <div className="flex flex-col gap-1">
          <Check label="REST API" hint="scripts and other systems calling /api" checked={value.api} invalid={!!errors.channels} onChange={(on) => set('api', on)} />
          <Check label="MCP" hint="MCP clients such as Claude Code, through /mcp" checked={value.mcp} invalid={!!errors.channels} onChange={(on) => set('mcp', on)} />
        </div>
        <FieldError id={channelsError} text={errors.channels} />
      </fieldset>

      <LimitChoice
        legend="Permissions"
        limited={value.limitPermissions}
        onLimited={(on) => set('limitPermissions', on)}
        all={words.everything}
        some="Only these"
        someDisabled={scope.permissions.length === 0 ? words.nothing : undefined}
        error={errors.permissions}
      >
        {defs.error ? (
          <p className="text-[12px] text-danger">
            Could not load the permissions. <Button size="sm" variant="ghost" onClick={defs.reload}>Try again</Button>
          </p>
        ) : defs.loading ? (
          <p className="text-[12px] text-faint">Loading permissions…</p>
        ) : (
          <>
            <div className="flex gap-2 text-[12px]">
              <button type="button" className="text-accent hover:underline" onClick={() => set('permissions', readOnly(scope))}>Read only</button>
              <button type="button" className="text-accent hover:underline" onClick={() => set('permissions', [])}>Clear</button>
            </div>
            <PermissionMatrix defs={heldDefs} value={value.permissions} onChange={(p) => set('permissions', p)} />
          </>
        )}
      </LimitChoice>

      {!!areas.error && <p className="text-[12px] text-danger">Could not load the functional areas, so the key keeps all of them.</p>}
      {offeredAreas.length > 0 && (
        <LimitChoice
          legend="Functional areas"
          limited={value.limitAreas}
          onLimited={(on) => set('limitAreas', on)}
          all={scope.allAreas ? 'All areas' : words.everyArea}
          some="Only these"
          error={errors.areas}
        >
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {offeredAreas.map((a) => (
              <Check
                key={a.id}
                label={a.name}
                checked={value.areaIds.includes(a.id)}
                invalid={!!errors.areas}
                onChange={(on) => set('areaIds', on ? [...value.areaIds, a.id] : value.areaIds.filter((id) => id !== a.id))}
              />
            ))}
          </div>
          <p className="text-[11.5px] text-faint">A key limited to areas does not see items that have no area.</p>
        </LimitChoice>
      )}
      <p className="text-[11.5px] text-faint">
        The key never gets more than its account: if the account loses a permission or area later, so do its keys. Schedules,
        triggers and chats set up with the key keep its limits, and stop when the key does.
      </p>
    </div>
  )
}

const toneFor = (limited: boolean): Tone => (limited ? 'blue' : 'neutral')

/** A key's limits as chips for key lists. */
export function KeyAccess({ k, areaName }: { k: ApiKeyLimits; areaName: (id: string) => string }) {
  const s = accessSummary(k, areaName)
  return (
    <div className="flex flex-wrap gap-1">
      <Chip tone={toneFor(!(k.allow_api && k.allow_mcp))}>{s.channels}</Chip>
      <Chip tone={toneFor(k.permissions !== null)} title={s.permissionsTitle}>{s.permissions}</Chip>
      <Chip tone={toneFor(k.area_ids !== null)} title={s.areasTitle}>{s.areas}</Chip>
    </div>
  )
}

/** Area names by id for key lists. Areas deleted since show as such; if the names cannot be loaded, ids stand in. */
export function useAreaNames(): (id: string) => string {
  const areas = useAsync((signal) => areasApi.list(signal), [])
  return (id) => {
    if (areas.error) return `Area ${id.slice(0, 8)}`
    if (!areas.data) return '…'
    return areas.data.find((a) => a.id === id)?.name ?? 'Deleted area'
  }
}
