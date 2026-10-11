import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { errorMessage } from '../../../api/client'
import { workOrdersApi } from '../../../api/endpoints'
import type { Run, WorkOrder, WorkOrderInput } from '../../../api/types'
import { NO_ERRORS, parseFieldErrors, type FieldErrors } from '../../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { deriveTargetInputs, valuesForInputs, type DerivedInputs, type TargetRef } from '../target/targetInputs'
import type { TargetCatalogData } from '../target/useTargetCatalog'
import { woNumber } from '../workOrderLabels'
import { hasTarget, normalizeWorkOrder, UNTITLED, validateWorkOrder, WORK_ORDER_FIELDS } from './composerForm'
import { useAttachmentQueue, type AttachmentQueue } from './useAttachmentQueue'

export type Composer = {
  value: WorkOrderInput
  set: <K extends keyof WorkOrderInput>(key: K, v: WorkOrderInput[K]) => void
  setTarget: (target: TargetRef) => void
  derived: DerivedInputs
  saved: WorkOrder | null
  dirty: boolean
  errors: FieldErrors
  busy: 'draft' | 'submit' | null
  queue: AttachmentQueue
  saveDraft: () => Promise<WorkOrder | null>
  submit: () => Promise<{ workOrder: WorkOrder; run: Run } | null>
}

const sameJson = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

/** State and actions for creating or editing a work order, including draft creation for file uploads. */
export function useComposer(workOrder: WorkOrder | null, initialValue: WorkOrderInput, catalog: TargetCatalogData): Composer {
  // A new work order with a preselected target starts with that target's input defaults
  const [initial] = useState(() => (workOrder ? initialValue : { ...initialValue, inputs: valuesForInputs(deriveTargetInputs(initialValue, catalog).inputs, initialValue.inputs) }))
  const [value, setValue] = useState<WorkOrderInput>(initial)
  const [baseline, setBaseline] = useState<WorkOrderInput>(initial)
  const [saved, setSaved] = useState<WorkOrder | null>(workOrder)
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [busy, setBusy] = useState<Composer['busy']>(null)
  const valueRef = useRef(value)
  valueRef.current = value
  const savedRef = useRef(saved)
  const draftPromise = useRef<Promise<string | null> | null>(null)

  const derived = useMemo(() => deriveTargetInputs(value, catalog), [value, catalog])
  const dirty = !sameJson(value, baseline)

  const set = useCallback(<K extends keyof WorkOrderInput>(key: K, v: WorkOrderInput[K]) => setValue((prev) => ({ ...prev, [key]: v })), [])
  const setTarget = useCallback(
    (target: TargetRef) => setValue((prev) => {
      const next = { ...prev, ...target }
      return { ...next, inputs: valuesForInputs(deriveTargetInputs(next, catalog).inputs, prev.inputs) }
    }),
    [catalog],
  )

  const fail = useCallback((action: string, err: unknown) => {
    const parsed = parseFieldErrors(err, WORK_ORDER_FIELDS)
    const general = parsed === NO_ERRORS ? [errorMessage(err)] : parsed.general
    setErrors({ fields: parsed.fields, general })
    notifyError(action, err)
  }, [])

  const persist = useCallback(async (body: WorkOrderInput): Promise<WorkOrder> => {
    const current = savedRef.current
    const wo = current ? await workOrdersApi.update(current.id, body) : await workOrdersApi.create(body)
    savedRef.current = wo
    setSaved(wo)
    setBaseline(body)
    return wo
  }, [])

  // Uploads need a work order id: create a draft the first time a file is attached (once a target is chosen)
  const ensureDraft = useCallback((): Promise<string | null> => {
    if (savedRef.current) return Promise.resolve(savedRef.current.id)
    const v = valueRef.current
    if (!hasTarget(v)) return Promise.resolve(null)
    draftPromise.current ??= (async () => {
      const title = v.title.trim() || UNTITLED
      if (!v.title.trim()) setValue((prev) => ({ ...prev, title }))
      try {
        const wo = await persist(normalizeWorkOrder({ ...v, title }))
        notifySuccess(`Saved draft ${woNumber(wo.number)} so your files can upload`)
        return wo.id
      } catch (err) {
        fail('Could not create a draft for the files', err)
        return null
      } finally {
        draftPromise.current = null
      }
    })()
    return draftPromise.current
  }, [persist, fail])

  const queue = useAttachmentQueue({ workOrderId: saved?.id ?? null, initial: workOrder?.attachments ?? [], ensureDraft })

  // Files attached before a target was chosen upload as soon as one is
  const waiting = queue.pending.filter((p) => p.status === 'waiting').length
  const targetChosen = hasTarget(value)
  const { flush } = queue
  useEffect(() => {
    if (waiting === 0 || !targetChosen) return
    void ensureDraft().then((id) => (id ? flush(id) : false))
  }, [waiting, targetChosen, ensureDraft, flush])

  const check = (forSubmit: boolean): boolean => {
    const local = validateWorkOrder(value, derived.inputs, forSubmit)
    if (Object.keys(local).length === 0) return true
    setErrors({ fields: local, general: [] })
    notifyError('Check the work order', new Error(Object.values(local).join(' · ')))
    return false
  }

  const saveDraft = async (): Promise<WorkOrder | null> => {
    if (!check(false)) return null
    setBusy('draft')
    try {
      const wo = await persist(normalizeWorkOrder(value))
      setErrors(NO_ERRORS)
      await flush(wo.id)
      notifySuccess(`Saved ${woNumber(wo.number)}`)
      return wo
    } catch (err) {
      fail('Could not save the work order', err)
      return null
    } finally {
      setBusy(null)
    }
  }

  const submit = async (): Promise<{ workOrder: WorkOrder; run: Run } | null> => {
    if (!check(true)) return null
    setBusy('submit')
    try {
      const wo = savedRef.current && !dirty ? savedRef.current : await persist(normalizeWorkOrder(value))
      if (!(await flush(wo.id))) {
        setErrors({ fields: {}, general: ['Some files have not uploaded. Retry or remove them, then submit again.'] })
        return null
      }
      const run = await workOrdersApi.submit(wo.id)
      setErrors(NO_ERRORS)
      return { workOrder: wo, run }
    } catch (err) {
      fail('Could not submit the work order', err)
      return null
    } finally {
      setBusy(null)
    }
  }

  return { value, set, setTarget, derived, saved, dirty, errors, busy, queue, saveDraft, submit }
}
