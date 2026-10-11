import { useCallback, useState } from 'react'
import { NO_ERRORS, parseFieldErrors, type FieldErrors } from './fieldErrors'
import { notifyError, notifySuccess } from './notify'

type EntityFormOptions<In, Out> = {
  entityId: string | null
  initial: In
  fields: readonly string[]
  label: string
  validate: (value: In) => Record<string, string>
  normalize?: (value: In) => In
  create: (body: In) => Promise<Out>
  update: (id: string, body: In) => Promise<Out>
}

export type EntityForm<In, Out> = {
  value: In
  set: <K extends keyof In>(key: K, v: In[K]) => void
  update: (fn: (prev: In) => In) => void
  errors: FieldErrors
  saving: boolean
  dirty: boolean
  save: () => Promise<Out | null>
  reset: (next: In) => void
}

/** Shared state for create/edit forms: local validation, 422 field mapping, dirty tracking. */
export function useEntityForm<In, Out>(opts: EntityFormOptions<In, Out>): EntityForm<In, Out> {
  const [baseline, setBaseline] = useState<In>(opts.initial)
  const [value, setValue] = useState<In>(opts.initial)
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [saving, setSaving] = useState(false)

  const set = useCallback(<K extends keyof In>(key: K, v: In[K]) => setValue((prev) => ({ ...prev, [key]: v })), [])
  const update = useCallback((fn: (prev: In) => In) => setValue(fn), [])
  const reset = useCallback((next: In) => {
    setBaseline(next)
    setValue(next)
    setErrors(NO_ERRORS)
  }, [])

  const save = async (): Promise<Out | null> => {
    const local = opts.validate(value)
    if (Object.keys(local).length > 0) {
      setErrors({ fields: local, general: [] })
      notifyError(`Check the ${opts.label} form`, new Error(Object.values(local).join(' · ')))
      return null
    }
    setSaving(true)
    const body = opts.normalize ? opts.normalize(value) : value
    try {
      const saved = opts.entityId ? await opts.update(opts.entityId, body) : await opts.create(body)
      setBaseline(body)
      setValue(body)
      setErrors(NO_ERRORS)
      notifySuccess(opts.entityId ? `Saved ${opts.label}` : `Created ${opts.label}`)
      return saved
    } catch (err) {
      setErrors(parseFieldErrors(err, opts.fields))
      notifyError(`Could not save ${opts.label}`, err)
      return null
    } finally {
      setSaving(false)
    }
  }

  const dirty = JSON.stringify(value) !== JSON.stringify(baseline)
  return { value, set, update, errors, saving, dirty, save, reset }
}
