// Trackers (Jira, Linear and KM2 issues): a crew's tracking list, a connection's people map, and roll-ups. See
// docs/crew-state-and-pmo-design.md, sections 10 and 12, and docs/trackers.md.

/** The connection kinds that are trackers */
export type TrackerKind = 'jira' | 'linear' | 'km2_tracker'

const TRACKER_LABELS: Record<TrackerKind, string> = { jira: 'Jira', linear: 'Linear', km2_tracker: 'KM2' }

/** Whether a connection kind is a tracker (Jira, Linear or KM2 issues). */
export function isTrackerKind(kind: string): kind is TrackerKind {
  return Object.hasOwn(TRACKER_LABELS, kind)
}

/** A tracker kind's short name, such as "Jira"; other kinds as they are. */
export function trackerKindLabel(kind: string): string {
  return isTrackerKind(kind) ? TRACKER_LABELS[kind] : kind
}

export type IncludeChildren = 'all' | 'direct' | 'none'

export type TrackingItem = {
  id: string
  crew_id: string
  schedule_id: string | null
  schedule_name: string
  connection_id: string
  connection_name: string
  connection_kind: TrackerKind
  project: string
  item_ref: string
  include_children: IncludeChildren
  note: string
  lead_account_id: string
  /** The lead's name from the connection's people map; empty when the account is not in it */
  lead_name: string
  added_by: string | null
  added_by_name: string
  added_at: string
  removed_at: string | null
  removed_by_name: string
}

export type TrackingItemInput = {
  connection_id: string
  item_ref: string
  project: string
  include_children: IncludeChildren
  note: string
  lead_account_id: string
  schedule_id: string | null
}

export type TrackerConnectionRef = { id: string; name: string; kind: TrackerKind }

export type TrackingView = { items: TrackingItem[]; connections: TrackerConnectionRef[] }

export type TrackerPerson = { account_id: string; name: string; email?: string }

export type TrackerIssue = {
  key: string
  title: string
  url: string
  status: string
  status_category: 'to_do' | 'in_progress' | 'done' | 'canceled'
  assignee: TrackerPerson | null
  due_date: string
  priority: string
  updated_at: string
  parent: string
  labels: string[]
  project: string
}

/** What adding an item would track, looked up in the tracker before the person confirms. */
export type TrackingCheck = {
  connection: TrackerConnectionRef
  issue: TrackerIssue
  direct_children: number
  children_capped: boolean
  already_tracked: boolean
}

export type PersonLink = '' | 'email' | 'manual'

export type PeopleMapEntry = {
  connection_id: string
  account_id: string
  display_name: string
  email: string
  user_id: string | null
  user_name: string
  link: PersonLink
  seen_at: string
  updated_at: string
}

export type LinkableUser = { id: string; name: string; email: string }

export type PeopleView = { people: PeopleMapEntry[]; users: LinkableUser[] }

export type PeopleSync = PeopleView & { read: number; linked: number }

export type NotifyPerson = { account_id: string; name: string; studio_user?: string }

/** One tracked item's roll-up, worked out by code from its child tickets. */
export type Rollup = {
  item: string
  title: string
  project: string
  connection: string
  url: string
  status: string
  due_date: string
  note?: string
  lead?: NotifyPerson
  children: { total: number; done: number; in_progress: number; to_do: number; canceled: number }
  overdue: string[]
  due_after_parent: { id: string; due_date: string }[]
  blocked: string[]
  unassigned: string[]
  stale: { id: string; last_update: string }[]
  new_comments: number
  changed_since_last_check: boolean
}

/** The "rollups" input of a tracking-list work order: only the tracked items with changes beneath them. */
export type RollupsInput = { items: Rollup[]; unchanged_items: number; stale_days: number; how_to_use?: string }
