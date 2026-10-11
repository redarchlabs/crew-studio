# Trackers: Jira Cloud, Linear and KM2 issues

Phase 4 of `crew-state-and-pmo-design.md`: tracker connections, the people map, the tracking list, tracker and
tracking-list watch sources with roll-ups, and the tracker tools. This file records the tracker API facts the clients
rely on (checked against the official documentation on 2026-10-05) and Studio's own endpoints. A third tracker kind,
`km2_tracker`, reads issues kept as custom entities in a KM2 organisation (see [KM2 issues](#km2-issues)).

Code: `server/internal/tracker` (clients, written by hand), `server/internal/watch/tracker.go`, `rollups.go` and
`comments.go` (sources), `server/internal/api/tracking.go`, `runtracker.go` and `runtracker_post.go` (endpoints),
`runner/studio_runner/tracker_tools.py` (agent tools). Migrations `0015_trackers.sql` and `0018_km2_tracker.sql`
(the `km2_tracker` connection kind).

## Verified API facts

Sources: the official Jira v3 OpenAPI spec (`https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json`),
the Jira REST v3 reference (`https://developer.atlassian.com/cloud/jira/platform/rest/v3/`), Linear's developer docs
(`https://linear.app/developers/graphql`, `/pagination`, `/filtering`, `/rate-limiting`) and Linear's published
schema (`https://raw.githubusercontent.com/linear/linear/master/packages/sdk/src/schema.graphql`). Anonymous calls to
Atlassian's public Cloud site confirmed the search response shape and that the old search endpoint is gone.

### Jira Cloud REST v3

| Topic | Fact | Source |
|---|---|---|
| Issue search | `GET /rest/api/3/search/jql` with `jql`, `nextPageToken`, `maxResults`, `fields` (comma list; the default is `id` only). Response `{ issues, nextPageToken, isLast }`; **no `total`**; `nextPageToken` is left out on the last page. Pages can hold fewer issues than asked for (100 with several fields), so the client loops until `isLast` and never assumes a page size. JQL must be bounded (a project or parent clause). | spec; live |
| Old search | `/rest/api/3/search` is deprecated and now answers **HTTP 410** ("migrate to /rest/api/3/search/jql"). Studio never calls it. | spec; live; changelog CHANGE-2046 |
| Auth | `Authorization: Basic base64(email:api_token)`. Classic tokens call `https://<site>.atlassian.net`; **scoped tokens must call `https://api.atlassian.com/ex/jira/{cloudId}`**. The cloud id is at `https://<site>.atlassian.net/_edge/tenant_info`. | developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/; support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/ |
| Fields | `status { name, statusCategory { key: new \| indeterminate \| done } }`, `assignee { accountId, displayName, emailAddress? }` (email often hidden by privacy settings), `duedate` (`YYYY-MM-DD` or null), `priority { name }`, `updated` (`2006-01-02T15:04:05.000-0700`, no colon in the offset), `parent { key }`, `labels []`, `project { key }`. | spec; live |
| Children | JQL `parent = KEY` works for epics and sub-tasks in team-managed and company-managed projects ("Epic Link" was retired in favour of Parent in 2024). Studio walks levels with it. | support.atlassian.com/jira-software-cloud/docs/jql-fields/ |
| Issue | `GET /rest/api/3/issue/{key}?fields=...` (includes `description` in ADF). | spec |
| Comments | `GET /rest/api/3/issue/{key}/comment?orderBy=-created&maxResults=N` returns `{ comments: [{ id, author { accountId, displayName }, created, body (ADF) }], total }`. `POST` the same path with `{ body: <ADF doc> }`. | spec |
| Mentions | ADF root `{ version: 1, type: "doc", content: [...] }`; a mention is an inline node `{ type: "mention", attrs: { id: <accountId>, text: "@Name" } }` inside a paragraph. Mentions need the account id, never a name or email. | developer.atlassian.com/cloud/jira/platform/apis/document/structure/ and /nodes/mention/ |
| Myself, projects | `GET /rest/api/3/myself`; `GET /rest/api/3/project/search?startAt&maxResults(<=100)&expand=lead` returns `{ values, isLast }`; `GET /rest/api/3/project/{key}` includes `lead`. | spec; live |
| People | `GET /rest/api/3/users/search?startAt&maxResults` returns a plain array of users with `accountType` (`atlassian` for people); `GET /rest/api/3/user?accountId=`. | spec |
| Create | `POST /rest/api/3/issue` with `{ fields: { project: { key }, summary, issuetype: { name }, description: <ADF>, parent?: { key } } }` answers `201 { id, key }`. | spec |
| Rate limits | `429` for any exceeded limit (`503` may also carry `Retry-After`); headers `Retry-After` (seconds), `X-RateLimit-Limit/Remaining/Reset`, `RateLimit-Reason`. Atlassian recommends honouring `Retry-After`, else exponential backoff from 2 s with jitter, capped near 30 s, a few retries. Points-based quotas (from March 2026) target apps; API-token traffic is reported as unaffected (not verified verbatim). | developer.atlassian.com/cloud/jira/platform/rate-limiting/ |

### Linear GraphQL

| Topic | Fact | Source |
|---|---|---|
| Endpoint and auth | `POST https://api.linear.app/graphql`; personal API keys go in `Authorization: <key>` **without** `Bearer` (OAuth tokens use `Bearer`). | linear.app/developers/graphql |
| Issues | `issue(id:)` accepts an identifier such as `ENG-123`. Fields: `identifier`, `title`, `url`, `state { name type }` with `type` one of triage, backlog, unstarted, started, completed, canceled, duplicate; `assignee { id name displayName email url }`; `dueDate` (TimelessDate, `YYYY-MM-DD`); `priorityLabel`; `updatedAt`; `parent { identifier }`; `children`; `team { key }`; `project { lead }`; `labels { nodes { name } }`; `description` (Markdown). | schema; linear.app/developers/graphql |
| Filtering | `issues(filter: IssueFilter, first, after)`; Studio uses `team: { key: { eq } }`, `parent: { id: { eq } }` and `state: { type: { nin: [...] } }`. | linear.app/developers/filtering; schema |
| Pagination | Relay connections: `first`/`after` with `pageInfo { hasNextPage endCursor }`; default page 50. The maximum `first` is not documented; Studio asks for 50. | linear.app/developers/pagination |
| Comments | `issue.comments(first, filter: CommentFilter, orderBy)`; `CommentFilter.createdAt: { gt: DateTimeOrDuration }`. A comment has `id body createdAt user { ... }` (`user` is null for bots). `commentCreate(input: { issueId, body })` returns `{ success comment { id url } }`; `issueId` may be an identifier. | schema; linear.app/developers/graphql |
| Mentions | Put the person's profile URL (`User.url`, such as `https://linear.app/<workspace>/profiles/<name>`) in the Markdown body; Linear turns it into a mention. `bodyData` is internal. | linear.app/developers/graphql ("Adding mentions in Markdown") |
| Other | `viewer { id name email }`; `teams(first, after)`; `users(first, after)`; `issueCreate(input: { teamId, title, description?, parentId? })`. | schema |
| Rate limits | API keys: 2,500 requests an hour per user and 3,000,000 complexity points an hour; at most 10,000 points per query. A limited request answers **HTTP 400** with `errors[].extensions.code = "RATELIMITED"` (the official SDK also treats 429 as limited); `X-RateLimit-Requests-Reset` is epoch milliseconds. | linear.app/developers/rate-limiting; SDK error.ts |
| Errors | `errors[]` with `message` and `extensions { code, type }`; a bad key answers HTTP 401 with `AUTHENTICATION_ERROR`. | linear.app/developers/graphql; live |

Not verified: the exact Jira CHANGE-2046 dates (removal itself was confirmed live), Linear's maximum `first`, the order
of `issue.comments(orderBy: createdAt)` (Studio sorts comments itself), and whether a Linear comment moves the
issue's `updatedAt` (Studio does not rely on it: Linear comments are read with the issue search, filtered by
`createdAt`).

## How the clients behave

- One `tracker.Client` interface; Jira, Linear and KM2 implementations. No SDKs.
- Every request goes to the connection's own host. Jira site URLs must be `https://<name>.atlassian.net` (no port or
  path), or an origin an administrator listed in `STUDIO_JIRA_SITES` (comma-separated, such as
  `https://jira.example.com`; plain http only for loopback). `STUDIO_LINEAR_API_URL` replaces the Linear endpoint
  (tests only; https unless loopback).
- No redirects are followed, so credentials never reach another host. The connection transport refuses link-local
  (cloud metadata), multicast and unspecified addresses and ignores proxies.
- 30-second timeout per request, 8 MB response cap, at most 3 retries on rate limits (`Retry-After`, Linear's reset
  time, or 2 s backoff doubling to 30 s with jitter).
- Errors are written by Studio: the tracker's status and its short error messages, never response bodies, URLs with
  secrets or the token itself.
- Status categories are folded into `to_do`, `in_progress`, `done` and `canceled` (Jira: done-category statuses named
  Canceled, Cancelled, Won't Do, Rejected or Duplicate count as canceled).
- "Blocked" means the status name or a label contains "block".
- A Jira watch's extra JQL must have balanced parentheses and quotes, so it stays inside `project = X AND (...)`.
- Run reads are throttled per connection (5 calls a second, burst 30) so one crew cannot use up a shared credential,
  and a children walk makes at most 500 searches.

## Endpoints

### Connections (`connections.view` / `connections.manage`)

- `POST /connections/{id}/test` for `jira` and `linear` asks whose account the credentials are (`/myself`, `viewer`):
  `{ supported: true, ok, message: "Connected as <name>.", status_code?, duration_ms }`. For `km2_tracker` it also
  checks the organisation's tracker entities: `"Connected as <name>. The organisation has the tracker entities."`, or
  `ok: false` with `"Connected as <name>, but the KM2 organisation is not ready: trk_issue lacks the fields labels;
  there is no trk_comment entity."`.
- Jira `config`: `site_url`, `email`, optional `cloud_id` (scoped tokens), `issue_type` (default Task) and
  `allow_create` (`"true"` lets crews create issues; off by default). Linear `config`: `allow_create`. A new
  `cloud_id` clears the saved token, like a new URL. `km2_tracker` `config`: see [KM2 issues](#km2-issues).
- `GET /connections/{id}/people` returns `{ people: [TrackerPerson], users: [{ id, name, email }] }` (active people in
  the connection's areas, for linking). `POST /connections/{id}/people/sync` reads the tracker's people (up to 2,000)
  and links unlinked accounts to Studio users with the same email: `{ people, users, read, linked }`.
  `PUT /connections/{id}/people/{accountId}` body `{ user_id: string | null }` links by hand (`link: "manual"`, never
  changed by a sync). Audited as `tracker_people_synced` and `tracker_person_linked`.

```ts
type TrackerPerson = { connection_id; account_id; display_name; email; user_id: string | null; user_name; link: "" | "email" | "manual"; seen_at; updated_at }
```

### Tracking list (`state.view` / `state.manage`)

- `GET /crews/{id}/state/tracking?include_removed=` returns `{ items: [TrackingItem], connections: [{ id, name, kind }] }`.
- `POST /crews/{id}/state/tracking/check` looks an item up (the crew may use the connection: linked, enabled, shares an
  area; the item exists) and answers `{ connection, issue, direct_children, children_capped, already_tracked }`.
- `POST /crews/{id}/state/tracking` adds it (`201`; `409 already_tracked`; `422 tracker_check_failed` when the item
  does not exist or the connection cannot be used; `502 tracker_failed` when the tracker failed).
- `DELETE /crews/{id}/state/tracking/{itemId}` removes it; it stays in the history.
- Lookups are limited to 20 a minute per person. Changes are audited (`tracking_item_added`, `tracking_item_removed`).

```ts
type TrackingItemInput = { connection_id; item_ref /* PRJ-10 */; project? /* empty: the project key */; include_children?: "all" | "direct" | "none"; note?; lead_account_id?; schedule_id?: string | null }
type TrackingItem = { id; crew_id; schedule_id: string | null; schedule_name; connection_id; connection_name; connection_kind; project; item_ref; include_children; note; lead_account_id; lead_name /* from the connection's people map; empty when the account is not in it */; added_by: string | null; added_by_name; added_at; removed_at: string | null; removed_by_name }
```

### Watches

Source kinds `tracker` (`project`, Jira-only `jql`, `include_done`, `lead`, `comments`, `stale_days`, and a tracker
`connection_id`: Jira, Linear or KM2 issues) and `tracking_list` (`comments`, `stale_days`). Items are identified by subject
(`jira:PRJ-15`); `tracked_fields` come from `title, status, status_category, assignee, due_date, priority, updated_at,
parent, labels`. A watch also has `mention_cap_per_day` (1 to 50, default 3) and `comment_cap_per_run` (1 to 500,
default 30). Work orders of a tracking-list watch get a `rollups` input with only the tracked items that changed.

New comments: the snapshot keeps, per item, the newest comment time already reported (under `$studio`, never
compared). A first check, or an item new to the watch, starts at the check's time. Jira comments are read per issue
when the issue's `updated` moved (at most 200 issues a check); Linear and KM2 comments come with the issue search. Comments
by the connection's own account are left out; excerpts are cut at 500 characters and at most 50 go to a run
(`new_comments_omitted` counts the rest).

The scheduler fires due schedules up to 4 at a time, each watch check within 3 minutes and each whole fire within 5,
so a slow tracker does not hold up other schedules. A check that starts no run still saves the snapshot when an item's
newest-comment time moved (for example the crew's own comments), so the same comments are not read every tick. Jira
reads up to 100 comments per issue; Linear brings up to 20 with the search and reads the rest separately. Roll-ups
count each item's new comments before the 50-per-check cap. An item tracked both crew-wide and for the schedule is
read once; the same key through two connections fails the check.

### Run state (run token)

| Call | Does |
|---|---|
| `GET /run-state/tracking` | the crew's tracking list (crew-wide items and the run's schedule's) |
| `GET /run-state/trackers` | the run's tracker connections (Jira, Linear, KM2 issues) |
| `GET /run-state/trackers/projects?connection=` | projects (Jira, KM2) or teams (Linear) |
| `GET /run-state/trackers/issues?project=&overdue=&due_within_days=&not_updated_days=&unassigned=&blocked=&owner=&include_done=&connection=` | `{ issues, total }`: the first 50 of up to 1,000 issues, filtered by code |
| `GET /run-state/trackers/issues/{ref}?connection=` | the issue with its 10 newest comments |
| `GET /run-state/trackers/issues/{ref}/children?depth=&connection=` | children, all levels (`depth=0`) or the first levels; at most 200 |
| `POST /run-state/trackers/issues/{ref}/comments` | body `{ text, mentions?: [accountId], followup_step?, connection? }` |
| `POST /run-state/trackers/issues/{ref}/suggestions` | body `{ field, value, reason, mentions?, connection? }`; posts a "suggested change" comment |
| `POST /run-state/trackers/issues` | body `{ project, title, description?, parent?, connection? }`; `403 create_off` unless `allow_create` |

`connection` is a connection id or name; it may be left out when the run has one tracker connection. Posts answer
`201 { posted: true, issue, url, logged, message }`, or in a dry run `200 { posted: false, dry_run: true, would_post:
{ connection, issue, mentions, text }, logged }`. Refusals: `429 comment_cap`, `429 mention_cap`,
`422 unknown_person` (a mention that is not in the people map, the issue's assignee or lead, or a tracking-list
lead), `422` for Linear profile links in the text, `403` for chat turns. Every post (or would-post) is written to the
action log under `followup:<step>` (with `followup_step`, at most once per run and issue), `tracker:comment`,
`tracker:suggest` or `tracker:create`, with `details.mentions` holding the account ids the per-person cap counts.
Read endpoints are gated per chat tool for chat turns.

Posts take a per-crew database advisory lock, so parallel tool calls cannot all pass the caps before any is logged.
Every post is idempotent within a run (a follow-up step once per issue; any other post once per issue, text and
mentions), so a tool call retried after a timeout is not posted twice. Once sent, a post and its log entry finish
on their own context (90 s) even if the caller gives up; the runner waits up to 120 s for posting tools. Refusals
name people by account id only, and the runner fences tracker error text as untrusted.

### Chat

A front desk gets `tracking_list_get`, `tracker_get_issue` and `tracker_find_issues` when the person chatting holds
`state.view` and the crew may use a Jira or Linear connection that person can see. Tracker text it reads
is fenced as untrusted data. Every tracker call in a chat turn (and the tracking list it reads) is limited to the
crew's connections that the person chatting can see at that moment, even when the crew's own areas reach more; a person
who has been disabled since gets none (`409 no_tracker`). It proposes changes, which the person confirms (both need `state.manage`):

| Type | Payload | Applies | Undo |
|---|---|---|---|
| `tracking_add` | `{ item_ref, project?, include_children?, note?, lead_account_id?, connection? }` (`connection`: a name or id among the crew's tracker connections the person can see; may be left out when there is one) | looks the item up in the tracker, then adds it in the person's name; result `{ tracking_item_id, item_ref, connection, undoable }` | removes it |
| `tracking_remove` | `{ item_ref, connection? }` | removes the tracked item (only through a connection the person can see) | puts it back |

## KM2 issues

A `km2_tracker` connection uses a KM2 organisation as a small Linear-style tracker: projects, issues, people and
comments are KM2 custom-entity records, read and written through KM2's enterprise API (`/api/v1`). It is its own
kind, not `km2`: a `km2` connection holds a key scoped to a KM2 profile, and KM2 never gives a scoped key the records
scopes, while a tracker needs an org-wide key. It counts as a tracker everywhere (watches, the tracking list, the
tracker tools, the PMO wizard). Code: `server/internal/tracker/km2.go`; the fake in `tracker/trackertest/km2.go`.

### Schema convention

The organisation holds these entities, all with the same slug prefix (`entity_prefix`, default `trk_`). Studio reads
the fields listed; others (cycles, links, history, estimates) may exist and are ignored.

| Entity | Fields Studio uses | Relationships (to-one) |
|---|---|---|
| `trk_person` | `name` (text, required), `email` (text, required, unique), `active` (boolean), `is_bot` (boolean) | |
| `trk_project` | `key` (text, required, unique, such as `ERP`), `name` | `lead` → person |
| `trk_issue` | `identifier` (text, required, unique, such as `ERP-12`), `number` (integer), `project_key` (text, required, a copy of the project's key), `title`, `description` (long text), `status` (picklist: Backlog, Todo, In Progress, In Review, Done, Canceled, Duplicate), `status_category` (picklist `to_do`, `in_progress`, `done`, `canceled`, required, kept in step with `status`), `priority` (picklist), `labels` (comma-separated text; a label containing "block" makes the issue blocked), `due_date` (date), `parent_key` (a copy of the parent's identifier), and the activity field when it is not `updated_at` | `project`, `parent` → issue, `assignee` → person, `creator` → person |
| `trk_comment` | `body` (long text, required), `issue_key`, `project_key`, `author_name`, `mentions` (comma-separated person ids) | `issue` (required), `author` → person |

Hard requirements, which **Test** checks: `trk_issue.status_category` is a picklist of exactly `to_do`,
`in_progress`, `done` and `canceled` (no other values, no other spellings), because open-issue searches filter on
`to_do,in_progress` in KM2 and Studio folds the same four values into its categories; any other set is a problem
and the connection is not ready. `trk_issue.identifier` must be unique (KM2's `is_unique`), so two writers cannot
make the same key; a non-unique identifier is a warning on Test. `trk_person.email` should be unique too.

KM2 returns relationships as bare record ids, hence the copied keys on issues (`project_key`, `parent_key`), which
Studio filters on and which must be filled in. Comments are found through their `issue` relationship, so a comment
made in KM2's own screens with only `issue` set is still read; Studio fills `issue_key`, `project_key` and
`author_name` on the comments it posts. One `trk_person` stands for Crew Studio (`is_bot: true`, its email is the
connection's `self_email`): comments are posted as it, its own comments are left out of watches, and the people map
leaves bot persons out.

### Connection settings

| Setting | |
|---|---|
| `base_url` (required) | The KM2 API address, such as `https://km2.example.com`. https only; plain http only to this machine (tests). No path. When `STUDIO_KM2_TRACKER_SITES` is set (comma-separated origins, such as `https://app.redarchlabs.com`), only those are allowed; unset, any https address is. |
| `self_email` (required) | The email of the Crew Studio person record. Saved in lower case and matched ignoring case. |
| `entity_prefix` | Slug prefix of the entities; default `trk_`. Like `base_url`, changing it clears the saved key. |
| `activity_field` | The issue field that says when an issue last changed (stale checks, `updated_at` in watches); default `updated_at`. A simulator may keep its own, such as `activity_at`. |
| `issue_url_template` | Optional link to an issue, with `{id}` (record id) or `{key}` (identifier), such as `https://km2.example.com/records/trk_issue?record_id={id}`. The placeholders may not be in the scheme or host; in the path they are path-escaped, in the query or fragment query-escaped. Without it issues have no link. |
| `allow_create` | `"true"` lets crews create issues (off by default). |
| secret `api_key` (required) | An org-wide KM2 API key (it must start with `km2_`) with `entities:read`, `records:read` and `records:write`. Mint it in KM2 under Admin → API in the organisation; not a profile key. |

Setting up: create the entities in the KM2 organisation (a configuration bundle or the KM2 admin UI), add the Crew
Studio person, mint the key, add the connection on Connections, and press **Test**: it reads the person by
`self_email` and `GET /api/v1/entities/{prefix}{person,project,issue,comment}`, and lists missing entities and fields
and a wrong `status_category`, with a warning for a non-unique `identifier` (KM2's catalog API does not list
relationships, so those are not checked).

Rolling back migration `0018_km2_tracker.sql` deletes every `km2_tracker` connection with its watches and
tracking-list items (crew links and people maps go with the connection).

### KM2 API facts used

From KM2's `routers/v1/entities.py`, `records.py`, `repositories/dynamic_entity.py` and `services/entity_records_helpers.py`
(KM2 main, b55e0d17).

| Topic | Fact |
|---|---|
| Auth | `Authorization: Bearer km2_...` (or `X-API-Key`). A bad key is `401 {"detail": "Invalid or missing API key"}`; a missing scope `403`. |
| Catalog | `GET /api/v1/entities/{slug}` (`entities:read`) → `{ id, name, slug, description, is_active, write_access, fields: [{ id, name, slug, field_type, picklist_options, is_required, is_unique, default_value, order, read_access }] }`; `404 {"detail": "entity not found"}`. |
| Records | `GET /api/v1/entities/{slug}/records` (`records:read`) with repeated `filter=<field>:<op>[:<value>]` (split at most twice, so timestamps keep their colons; ops `eq ne gt gte lt lte in contains isnull`; `in` takes up to 200 comma-separated values; base columns `id`, `created_at`, `updated_at` too), `order_by` (any field or base column, default `created_at`), `order_dir` (`asc`/`desc`, default `desc`; ties broken by `id` descending; nulls first descending, last ascending), `limit` (1 to 200, default 50) and an opaque `cursor` → `{ items, next_cursor, limit }`. A cursor is tied to its sort. Unknown fields are `400 {"detail": "unknown field: 'x'"}`. |
| Writes | `POST /api/v1/entities/{slug}/records` (`records:write`) with a slug-keyed body → `201` and the record; unknown fields `400 "unknown fields: [...]"`; picklist values must match exactly. `GET .../records/{id}` → the record or `404 "record not found"`. |
| Records | `{ id, created_at, updated_at, <field slugs>, <relationship slugs>: record id }`; timestamps in ISO 8601 with an offset. |
| Limits | 600 requests a minute per key and 1,200 per address; `429` with `Retry-After` (waited out like other trackers). |
| Unique fields | A duplicate `identifier` is a `409` from KM2 2.6.2; before it, an unhandled `500`. |

### How the KM2 client behaves

- People: the whole `trk_person` list is read once (up to 5,000) and kept per connection for the server's life; a
  person missing from it makes the list be read again, at most every 5 minutes. An organisation with more people is
  read by id instead (`id:in:` up to 200 ids a call), keeping what was found, so a large organisation never fails a
  search. Names from KM2 lose control characters and line breaks and are cut to 120 characters before Studio writes
  them into a comment.
- Projects: `trk_project` by `key`; the lead from the person cache.
- Issues: `trk_issue` by `identifier:eq:<REF>`. A search filters `project_key:eq:<KEY>` (or `parent:eq:<record id>`
  for children) and, for open issues, `status_category:in:to_do,in_progress`, paging 200 at a time by `created_at`;
  more than the caller's maximum is a limit error.
- `UpdatedAt` is the activity field (falling back to `updated_at`). Comment times are the comments' real `created_at`,
  never a simulated time.
- New comments come with the search: `trk_comment` with `issue:in:` (up to 200 of the found issues' record ids a call)
  and `created_at:gt:<since>`, at most 10,000 across all calls, so every issue has its comments read (`CommentsRead`).
  An issue's own comments are read with `issue:eq:<record id>`. KM2's filter parser accepts relationship fields
  (they are coerced to record ids). Comments do not move an issue's time. A comment whose `created_at` cannot be read
  is logged with its record id and never reported or counted as seen by a watch.
- Posting: a `trk_comment` with `body`, `issue`, `issue_key`, `project_key`, `author` (the Crew Studio person) and
  `author_name`. KM2 has no mentions: the people are named as `@Name` at the top of the body and their ids kept in
  `mentions`; nobody is notified by KM2.
- Creating (when allowed): the project's highest `number` plus one, `identifier` `<KEY>-<n>`, status Backlog
  (`to_do`), `creator` the Crew Studio person, and the activity field set to now when it is a field of its own. A
  `409` (a taken key or another unique field) tries the next number after 300 ms, then 600 ms (3 tries in all). A
  `500` first looks for an issue in the project with the same title, made by the Crew Studio person in the last few
  minutes, and returns it instead of creating a duplicate; otherwise it is tried once more, then fails clearly.
- Errors repeat KM2's `detail` only when the answer is KM2's JSON (a string, or a list of validation messages),
  without control characters and cut to 300 characters; anything else is reported by its status alone.
- Studio never edits or deletes a record. A missing entity or field fails with a message pointing at Test.
