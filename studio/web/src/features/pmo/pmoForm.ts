import type { Permission } from '../../api/types.access'
import type { KM2FolderList } from '../../api/types.crewstate'
import type { PMOChoice, PMOOptions, PMORequest } from '../../api/types.delivery'
import { toInputs, validateRows, type DestinationRow } from '../delivery/destinationsForm'

/** Setting up a PMO creates agents, tasks, a crew and schedules, so it needs each permission (server: pmo.Needs). */
export const PMO_PERMISSIONS: Permission[] = ['crews.create', 'crews.edit', 'agents.create', 'tasks.create', 'schedules.manage', 'work_orders.create', 'connections.view']

export const STEPS = ['Basics', 'Tracker', 'Report destinations', 'Models and review'] as const
export type Step = 0 | 1 | 2 | 3

export type PMOForm = {
  name: string
  area_ids: string[]
  timezone: string
  tracker_connection_id: string
  destinations: DestinationRow[]
  coordinator_llm_id: string
  reporter_llm_id: string
  enabled: boolean
}

/**
 * The suggested name: after the tracker connection followed, or "Team PMO". Plain "PMO" would name the coordinator
 * "PMO Coordinator", which the built-in template already uses.
 */
export function suggestedName(tracker: PMOChoice | undefined): string {
  return tracker ? `${tracker.name} PMO` : 'Team PMO'
}

export function emptyForm(areaIds: string[], timezone: string, opts: PMOOptions): PMOForm {
  const tracker = opts.trackers.length === 1 ? opts.trackers[0] : undefined
  return {
    name: suggestedName(tracker), area_ids: areaIds, timezone, tracker_connection_id: tracker?.id ?? '', destinations: [],
    coordinator_llm_id: opts.coordinator_llm_id ?? '', reporter_llm_id: opts.reporter_llm_id ?? '', enabled: true,
  }
}

/** Connections in at least one of the chosen areas (a connection with no areas listed is kept: the server decides). */
export function inAreas(choices: readonly PMOChoice[], areaIds: readonly string[]): PMOChoice[] {
  if (areaIds.length === 0) return [...choices]
  return choices.filter((c) => !c.area_ids || c.area_ids.some((a) => areaIds.includes(a)))
}

/** Problems on one step, keyed by field ("destinations.<i>" for destination rows). */
export function validateStep(step: Step, f: PMOForm): Record<string, string> {
  const errors: Record<string, string> = {}
  if (step === 0) {
    if (!f.name.trim()) errors.name = 'Give the PMO a name'
    else if (f.name.trim().length > 80) errors.name = 'At most 80 characters'
    if (f.area_ids.length === 0) errors.area_ids = 'Pick at least one functional area'
    if (!f.timezone) errors.timezone = 'Choose a time zone'
  }
  if (step === 1 && !f.tracker_connection_id) errors.tracker_connection_id = 'Choose the tracker connection to follow'
  if (step === 2) {
    for (const [i, msg] of Object.entries(validateRows(f.destinations))) errors[`destinations.${i}`] = msg
  }
  if (step === 3) {
    if (!f.coordinator_llm_id) errors.coordinator_llm_id = 'Choose an LLM'
    if (!f.reporter_llm_id) errors.reporter_llm_id = 'Choose an LLM'
  }
  return errors
}

/**
 * KM2 destinations that could not file anything: no folder picked and a connection without a default folder. A
 * connection whose folders cannot be listed is let through (the server checks the folder when saving).
 */
export async function km2DestinationProblems(rows: readonly DestinationRow[], names: Record<string, string>,
  load: (connectionId: string) => Promise<KM2FolderList>): Promise<Record<string, string>> {
  const errors: Record<string, string> = {}
  await Promise.all(rows.map(async (r, i) => {
    if (r.kind !== 'km2' || !r.connection_id || r.folder.trim()) return
    try {
      const list = await load(r.connection_id)
      if (list.ok && !list.allowed.default_folder_id) {
        errors[`destinations.${i}`] = `${names[r.connection_id] ?? 'This connection'} has no default folder, so reports cannot be filed: pick a folder, or set a default folder on the connection.`
      }
    } catch {
      // Unknown: the server decides
    }
  }))
  return errors
}

/** The Name problem in a setup error ("name: …"), so the wizard can show it on the Basics step. */
export function nameProblem(details: readonly string[]): string | undefined {
  const d = details.find((x) => x.startsWith('name: '))
  return d?.slice('name: '.length)
}

/** Row errors of the destinations step, by row index. */
export function rowErrors(errors: Record<string, string>): Record<number, string> {
  const out: Record<number, string> = {}
  for (const [key, msg] of Object.entries(errors)) {
    if (key.startsWith('destinations.')) out[Number(key.slice('destinations.'.length))] = msg
  }
  return out
}

export function toRequest(f: PMOForm): PMORequest {
  return {
    name: f.name.trim(), area_ids: f.area_ids, timezone: f.timezone, tracker_connection_id: f.tracker_connection_id,
    destinations: toInputs(f.destinations), coordinator_llm_id: f.coordinator_llm_id || null, reporter_llm_id: f.reporter_llm_id || null,
    enabled: f.enabled,
  }
}
