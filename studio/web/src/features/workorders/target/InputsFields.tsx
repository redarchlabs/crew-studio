import { X } from 'lucide-react'
import { IconButton } from '../../../components/Button'
import { TextAreaField } from '../../../components/Field'
import type { DerivedInputs } from './targetInputs'
import { extraKeys } from './targetInputs'

type InputsFieldsProps = {
  derived: DerivedInputs
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
  errors?: Record<string, string>
  disabled?: boolean
  /** Shown when no crew is chosen yet */
  noTarget?: boolean
}

/** One field per input the target crew declares. */
export function InputsFields({ derived, values, onChange, errors = {}, disabled, noTarget }: InputsFieldsProps) {
  const extras = extraKeys(derived.inputs, values)
  const set = (key: string, v: string) => onChange({ ...values, [key]: v })
  const drop = (key: string) => onChange(Object.fromEntries(Object.entries(values).filter(([k]) => k !== key)))

  if (noTarget) return <p className="text-[12px] text-faint">Choose a crew to see the inputs it needs.</p>

  return (
    <div className="flex flex-col gap-3">
      {derived.inputs.length === 0 && extras.length === 0 && <p className="text-[12px] text-faint">This crew takes no inputs.</p>}
      {derived.inputs.map((spec) => (
        <TextAreaField
          key={spec.key}
          rows={(values[spec.key] ?? '').length > 80 ? 3 : 1}
          disabled={disabled}
          label={
            <>
              {spec.label} <span className="font-mono text-faint">{`{${spec.key}}`}</span>
              {spec.required && <span className="text-danger"> *</span>}
            </>
          }
          value={values[spec.key] ?? ''}
          onChange={(v) => set(spec.key, v)}
          error={errors[spec.key]}
        />
      ))}
      {extras.length > 0 && (
        <div className="flex flex-col gap-1.5 rounded-md border border-dashed border-line p-2.5">
          <div className="text-[12px] text-muted">Saved inputs the crew no longer uses (kept unless you remove them):</div>
          {extras.map((k) => (
            <div key={k} className="flex items-center gap-2 text-[12px]">
              <span className="font-mono text-accent">{k}</span>
              <span className="min-w-0 flex-1 truncate text-muted">{values[k] || '""'}</span>
              {!disabled && <IconButton label={`Remove ${k}`} icon={<X className="size-3.5" />} onClick={() => drop(k)} />}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
