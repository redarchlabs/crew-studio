import { Link } from 'react-router'
import { isTrackerKind } from '../../../api/types.trackers'
import type { WatchConnection } from '../../../api/types.watch'
import { NumberField, SelectField, TextField, Toggle } from '../../../components/Field'
import { DEFAULT_TRACKER_FIELDS, TRACKER_FIELDS, type WatchForm } from './watchForm'

type Props = {
  form: WatchForm
  set: <K extends keyof WatchForm>(key: K, value: WatchForm[K]) => void
  connections: WatchConnection[]
  crewId: string | null
  errors: Record<string, string>
}

const stateTab = (crewId: string | null, text: string) => crewId && <Link className="text-accent" to={`/crews/${crewId}?tab=state`}>{text}</Link>

/** A tracker source: a Jira project, Linear team or KM2 project read through one of the crew's tracker connections. */
function ProjectFields({ form, set, connections, crewId, errors }: Props) {
  const trackers = connections.filter((c) => isTrackerKind(c.kind))
  const isJira = trackers.find((c) => c.id === form.connection_id)?.kind === 'jira'
  return (
    <>
      <SelectField
        label="Connection"
        value={form.connection_id}
        onChange={(v) => set('connection_id', v)}
        options={[{ value: '', label: trackers.length ? 'Choose a connection…' : 'No tracker connections for this crew' }, ...trackers.map((c) => ({ value: c.id, label: c.name }))]}
        error={errors.connection_id}
        hint={<>The crew&apos;s enabled tracker connections (Jira, Linear and KM2 issues). {stateTab(crewId, "Choose them on the crew's State tab.")}</>}
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <TextField label="Project" value={form.project} onChange={(v) => set('project', v)} error={errors.project} placeholder="PRJ" hint="The Jira project key, Linear team key or KM2 project key." />
        <TextField label="Lead (optional)" value={form.lead} onChange={(v) => set('lead', v)} placeholder="account id" hint="Who lead follow-ups go to instead of the project lead: a tracker account id (see the connection's people map)." />
      </div>
      {isJira && <TextField label="Extra JQL (optional)" value={form.jql} onChange={(v) => set('jql', v)} placeholder="labels = atlas" hint="ANDed with the project. No ORDER BY." />}
      <Toggle label="Include done issues" checked={form.include_done} onChange={(v) => set('include_done', v)} hint="Off: only open issues are read, and an issue that is done counts as removed." />
    </>
  )
}

/** The fields of tracker and tracking-list sources: what to read, comments, and which fields to compare. */
export function TrackerSourceFields(props: Props) {
  const { form, set, crewId, errors } = props
  return (
    <>
      {form.kind === 'tracker' ? (
        <ProjectFields {...props} />
      ) : (
        <p className="text-[12.5px] text-muted">
          Reads every item on the crew&apos;s tracking list with its child tickets (all levels or direct only, as each entry says), and gives the run a
          roll-up per tracked item. {stateTab(crewId, 'Manage the tracking list on the State tab.')}
        </p>
      )}
      <Toggle label="New comments" checked={form.comments} onChange={(v) => set('comments', v)}
        hint="Report comments added since the last check (500 characters each, 50 per check). Comments by the connection's own account are left out." />
      <div className="grid gap-3 sm:grid-cols-2">
        <TextField label="Tracked fields" value={form.tracked_fields} onChange={(v) => set('tracked_fields', v)} error={errors.tracked_fields}
          placeholder={DEFAULT_TRACKER_FIELDS.join(', ')} hint={`Any of ${TRACKER_FIELDS.join(', ')}. Empty: ${DEFAULT_TRACKER_FIELDS.join(', ')}.`} />
        <NumberField label="Stale after (days)" value={form.stale_days} onChange={(v) => set('stale_days', v)} nullable min={1} max={365} error={errors.stale_days}
          hint="Default 14. An open child not updated for this long is stale in roll-ups." />
        <NumberField label="Max items" value={form.max_items} onChange={(v) => set('max_items', v)} nullable min={1} max={10000} error={errors.max_items} hint="Default 2,000. More items fail the check instead of being cut." />
      </div>
    </>
  )
}

/** Limits on what the crew's tracker tools may post in this schedule's runs. */
export function WatchCapsFields({ form, set, errors }: Pick<Props, 'form' | 'set' | 'errors'>) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <NumberField label="Mentions per person per day" value={form.mention_cap} onChange={(v) => set('mention_cap', v)} nullable min={1} max={50} error={errors.mention_cap}
        hint="Default 3. The crew cannot @mention one person more often than this in 24 hours." />
      <NumberField label="Comments per run" value={form.comment_cap} onChange={(v) => set('comment_cap', v)} nullable min={1} max={500} error={errors.comment_cap}
        hint="Default 30. Further comments are refused; the crew puts them in its output." />
    </div>
  )
}
