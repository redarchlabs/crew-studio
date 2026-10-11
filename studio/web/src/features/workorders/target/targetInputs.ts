import type { Crew } from '../../../api/types'
import { labelFromKey } from '../../../lib/placeholders'

// Works out which inputs a work order, schedule or trigger needs from its target crew: the inputs the
// crew declares (api.md, Work orders).

export type InputSpec = {
  key: string
  label: string
  default: string
  required: boolean
}

/** What a work order, schedule or trigger runs: always a crew (null until one is chosen). */
export type TargetRef = { crew_id: string | null }

export type DerivedInputs = { inputs: InputSpec[] }

const EMPTY: DerivedInputs = { inputs: [] }

export function deriveCrewInputs(crew: Crew): DerivedInputs {
  return { inputs: crew.inputs.map((i) => ({ key: i.key, label: i.label || labelFromKey(i.key), default: i.default, required: i.required })) }
}

export type TargetCatalog = { crews: readonly Crew[] }

export function deriveTargetInputs(target: TargetRef, catalog: TargetCatalog): DerivedInputs {
  const crew = target.crew_id ? catalog.crews.find((c) => c.id === target.crew_id) : undefined
  return crew ? deriveCrewInputs(crew) : EMPTY
}

/**
 * Values for a (possibly new) set of inputs: keeps what the person typed for keys that still apply and fills
 * the rest with defaults. Keys the target no longer uses are dropped.
 */
export function valuesForInputs(specs: readonly InputSpec[], current: Readonly<Record<string, string>>): Record<string, string> {
  return Object.fromEntries(specs.map((s) => [s.key, current[s.key] ?? s.default]))
}

/** Required inputs left empty, as "label is required" messages keyed by input key. */
export function missingRequired(specs: readonly InputSpec[], values: Readonly<Record<string, string>>): Record<string, string> {
  return Object.fromEntries(specs.filter((s) => s.required && !(values[s.key] ?? '').trim()).map((s) => [s.key, `${s.label} is required`]))
}

/** Keys present in the values that the target does not use (e.g. after the target was edited). */
export function extraKeys(specs: readonly InputSpec[], values: Readonly<Record<string, string>>): string[] {
  const known = new Set(specs.map((s) => s.key))
  return Object.keys(values).filter((k) => !known.has(k))
}
