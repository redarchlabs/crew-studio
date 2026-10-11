import { useId, type ReactNode, type InputHTMLAttributes, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'

type FieldProps = {
  label: ReactNode
  error?: string
  hint?: ReactNode
  aside?: ReactNode
  children: (id: string, invalid: boolean) => ReactNode
  className?: string
}

/** Label + control + hint/error, wiring ids for accessibility. */
export function Field({ label, error, hint, aside, children, className = '' }: FieldProps) {
  const id = useId()
  return (
    <div className={`flex min-w-0 flex-col gap-1 ${className}`}>
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={id} className="text-[12px] font-medium text-muted">
          {label}
        </label>
        {aside}
      </div>
      {children(id, Boolean(error))}
      {error ? (
        <div className="text-[12px] text-danger" role="alert">
          {error}
        </div>
      ) : (
        hint && <div className="text-[11.5px] text-faint">{hint}</div>
      )}
    </div>
  )
}

type TextFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'onChange' | 'value'> & {
  label: ReactNode
  value: string
  onChange: (value: string) => void
  error?: string
  hint?: ReactNode
}

export function TextField({ label, value, onChange, error, hint, className, ...rest }: TextFieldProps) {
  return (
    <Field label={label} error={error} hint={hint} className={className}>
      {(id, invalid) => (
        <input id={id} className="field-input" aria-invalid={invalid} value={value} onChange={(e) => onChange(e.target.value)} {...rest} />
      )}
    </Field>
  )
}

type NumberFieldProps = {
  label: ReactNode
  value: number | null
  onChange: (value: number | null) => void
  nullable?: boolean
  error?: string
  hint?: ReactNode
  min?: number
  max?: number
  step?: number | 'any'
  placeholder?: string
  className?: string
  required?: boolean
}

/** Numeric input; empty means null when nullable, otherwise keeps the field empty until valid. */
export function NumberField({ label, value, onChange, nullable, error, hint, className, ...rest }: NumberFieldProps) {
  return (
    <Field label={label} error={error} hint={hint} className={className}>
      {(id, invalid) => (
        <input
          id={id}
          type="number"
          inputMode="decimal"
          className="field-input tabular"
          aria-invalid={invalid}
          value={value === null || Number.isNaN(value) ? '' : value}
          placeholder={nullable ? 'default' : undefined}
          onChange={(e) => {
            const raw = e.target.value
            if (raw === '') onChange(nullable ? null : Number.NaN)
            else onChange(Number(raw))
          }}
          {...rest}
        />
      )}
    </Field>
  )
}

type TextAreaFieldProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'onChange' | 'value'> & {
  label: ReactNode
  value: string
  onChange: (value: string) => void
  error?: string
  hint?: ReactNode
}

export function TextAreaField({ label, value, onChange, error, hint, className, rows = 4, ...rest }: TextAreaFieldProps) {
  return (
    <Field label={label} error={error} hint={hint} className={className}>
      {(id, invalid) => (
        <textarea id={id} rows={rows} className="field-input resize-y" aria-invalid={invalid} value={value} onChange={(e) => onChange(e.target.value)} {...rest} />
      )}
    </Field>
  )
}

export type SelectOption = { value: string; label: string; disabled?: boolean }

type SelectFieldProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, 'onChange' | 'value'> & {
  label: ReactNode
  value: string
  onChange: (value: string) => void
  options: readonly SelectOption[]
  error?: string
  hint?: ReactNode
  aside?: ReactNode
}

export function SelectField({ label, value, onChange, options, error, hint, aside, className, ...rest }: SelectFieldProps) {
  return (
    <Field label={label} error={error} hint={hint} aside={aside} className={className}>
      {(id, invalid) => (
        <select id={id} className="field-input" aria-invalid={invalid} value={value} onChange={(e) => onChange(e.target.value)} {...rest}>
          {options.map((o) => (
            <option key={o.value} value={o.value} disabled={o.disabled}>
              {o.label}
            </option>
          ))}
        </select>
      )}
    </Field>
  )
}

type ToggleProps = { label: ReactNode; checked: boolean; onChange: (checked: boolean) => void; hint?: ReactNode; disabled?: boolean }

export function Toggle({ label, checked, onChange, hint, disabled }: ToggleProps) {
  return (
    <label className={`flex cursor-pointer items-start gap-2.5 ${disabled ? 'opacity-50' : ''}`}>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`relative mt-0.5 inline-flex h-4 w-7 shrink-0 items-center rounded-full border transition-colors ${checked ? 'border-accent bg-accent' : 'border-line-strong bg-panel-2'}`}
      >
        <span className={`absolute size-3 rounded-full bg-white shadow transition-transform ${checked ? 'translate-x-3.5' : 'translate-x-0.5'}`} />
      </button>
      <span className="min-w-0">
        <span className="block text-[13px]">{label}</span>
        {hint && <span className="block text-[11.5px] text-faint">{hint}</span>}
      </span>
    </label>
  )
}

export function FormErrors({ messages }: { messages: readonly string[] }) {
  if (messages.length === 0) return null
  return (
    <ul className="space-y-0.5 rounded-md border border-danger/40 bg-danger-soft px-3 py-2 text-[12px] text-danger" role="alert">
      {messages.map((m) => (
        <li key={m}>{m}</li>
      ))}
    </ul>
  )
}
