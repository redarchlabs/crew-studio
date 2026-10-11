# Report delivery: Slack, Microsoft Teams, email and KM2

Phase 5 of `crew-state-and-pmo-design.md` (section 12): one report, sent to the work order output and every destination
set on the schedule that started the run. This file records the facts about Slack and Teams the senders rely on
(checked against the official documentation on 2026-10-05) and how Studio sends.

Code: `server/internal/delivery` (Slack, Teams and SMTP senders, written by hand; `deliverytest` holds the in-process
fakes), `server/internal/reports` (fan-out, action log, retry), `server/internal/api/runreports.go`,
`reportdestinations.go`, `workorderreports.go` and `deliverytests.go`, `runner/studio_runner/report_tools.py`
(`deliver_report`). Migration `0016_pmo_and_report_delivery.sql`. API: `docs/api.md` ("Report destinations", "Report
delivery", "PMO setup").

## Verified facts

### Microsoft Teams

| Topic | Fact | Source |
|---|---|---|
| Office 365 connectors | The connectors (the old "Incoming Webhook") were retired: the final rollout was set for 18 to 22 May 2026, after which "Office 365 Connectors will no longer function" (the date had moved several times since 2024). Studio refuses `*.webhook.office.com` and `outlook.office.com` URLs with that explanation. | https://devblogs.microsoft.com/microsoft365dev/retirement-of-office-365-connectors-within-microsoft-teams/ |
| Replacement | Workflows (Power Automate): create a workflow with the "When a Teams webhook request is received" trigger, or from a template such as "Send webhook alerts to a channel". | https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook ; https://support.microsoft.com/en-us/office/send-messages-in-teams-using-incoming-webhooks-323660ec-12ca-40b1-a1d3-a3df47e808c4 |
| Request body | `{"type": "message", "attachments": [{"contentType": "application/vnd.microsoft.card.adaptive", "contentUrl": null, "content": {"$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "type": "AdaptiveCard", "version": "1.2", "body": [{"type": "TextBlock", "text": "..."}]}}]}`. `type` must be `message`; `contentUrl` must be null. Studio sends exactly this shape (version 1.2, as Microsoft's sample) with three TextBlocks: the title, the report and a footer. | https://learn.microsoft.com/en-us/connectors/teams/ (Microsoft Teams Webhook) |
| Authentication | "Do not pass an authentication token header if you selected the 'Anyone' authentication option, or POST requests to the trigger will fail"; the tenant-only options need a token. Studio sends no token, so the workflow must allow **Anyone**; the URL itself carries the signature (`sig=`), so it is stored as a secret. | https://learn.microsoft.com/en-us/connectors/teams/ ; https://learn.microsoft.com/en-us/power-automate/oauth-authentication ; https://learn.microsoft.com/en-us/power-automate/regenerate-sas-key |
| Message size | About 28 KB ("If the message size exceeds 28 KB, the action fails with the error 'Request Entity too large'"). Studio cuts the report text to 20,000 bytes with a note pointing to Crew Studio. | https://learn.microsoft.com/en-us/connectors/teams/ |
| URL change | "Starting on November 30, 2025, Power Automate flows with HTTP triggers or Teams Webhook triggers that have `logic.azure.com` in the URL move to a new URL ... the old URLs stop working". The new URL "might be longer than 255 characters". From 2 June 2026 a scale-unit segment may appear in the path (`/direct/cu/20/workflows/...`); the old form keeps working. Studio refuses `*.logic.azure.com` with that explanation and allows secrets up to 8 KB. | https://learn.microsoft.com/en-us/troubleshoot/power-platform/power-automate/flow-run-issues/triggers-troubleshoot |
| Response | The trigger accepts the request; any 2xx counts as delivered. The flow posts to the channel afterwards, so a failure inside the flow is not reported back to Studio. | https://learn.microsoft.com/en-us/connectors/teams/ (the exact status code is not stated on the page; see "Not verified") |
| Graph alternative | `POST /teams/{team-id}/channels/{channel-id}/messages` needs the delegated `ChannelMessage.Send` permission; application permissions are only for migration (`Teamwork.Migrate.All`). A server posting on its own would need a signed-in user's token, so Studio does not use Graph. | https://learn.microsoft.com/en-us/graph/api/channel-post-messages?view=graph-rest-1.0 |

### Slack

| Topic | Fact | Source |
|---|---|---|
| Incoming webhook | `POST https://hooks.slack.com/services/T.../B.../...` with `Content-type: application/json` and `{"text": "..."}`; success is HTTP 200 with the body `ok`. The URL is a secret. The channel, user name and icon cannot be overridden. Errors are HTTP 400, 403 or 404 with a short code such as `invalid_payload`, `channel_not_found`, `channel_is_archived` or `action_prohibited`. Studio only accepts `hooks.slack.com` (or an origin in `STUDIO_DELIVERY_ORIGINS`) and shows only that short code. | https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks |
| chat.postMessage | `POST https://slack.com/api/chat.postMessage`, token as `Authorization: Bearer xoxb-...`, scope `chat:write`, JSON `{channel, text}`. Every answer has `ok`; on failure `error` holds a code such as `not_in_channel`, `channel_not_found`, `invalid_auth`, `missing_scope` or `is_archived`. `channel` may be an ID or a name; the bot must be in the channel. | https://docs.slack.dev/reference/methods/chat.postMessage ; https://docs.slack.dev/apis/web-api/ |
| Length | "Slack will truncate messages containing more than 40,000 characters." Studio cuts at 12,000 characters with a note. | https://docs.slack.dev/reference/methods/chat.postMessage |
| Rate limits | Incoming webhooks: 1 per second, short bursts allowed; chat.postMessage: about 1 message per second per channel. Over a limit Slack answers HTTP 429 with `Retry-After`. Studio reports a 429 as a retryable failure with the wait. | https://docs.slack.dev/apis/web-api/rate-limits |
| Formatting | Slack's mrkdwn is not Markdown: `*bold*`, `_italic_`, `<url|text>` links, no headings; `&`, `<` and `>` must be escaped. Studio converts headings and `**bold**` to `*bold*`, links to `<url|text>`, bullets to `•`, and escapes the rest. | https://docs.slack.dev/messaging/formatting-message-text |

Not verified: the HTTP status the Teams trigger answers (202 Accepted was reported by other pages but not stated on the
connector page; Studio accepts any 2xx), the exact host of the new Workflows URLs on Microsoft's own pages (third-party
pages and Microsoft Q&A show `*.environment.api.powerplatform.com`; Studio accepts `*.powerplatform.com`, plus any origin an
administrator lists in `STUDIO_DELIVERY_ORIGINS` if Microsoft moves the URLs again), whether
chat.postMessage always answers HTTP 200 on errors (Studio reads `ok` whatever the status), and the status codes of each
webhook error.

## How the senders behave

- Every request goes through the connection transport: no proxy, and no link-local (cloud metadata), multicast or
  unspecified addresses, checked when connecting. No redirect is followed (a redirect is reported as a failure). 30-second
  limit per send.
- Errors are written by Studio: the service's status and short error code, never a webhook URL, token, response body or
  SMTP reply text. Each failure says whether retrying can help.
- Webhook URLs must be https. Plain http is accepted only for an origin listed in `STUDIO_DELIVERY_ORIGINS` (tests).
  Slack webhooks must be on `hooks.slack.com` and Teams workflow URLs on a `*.powerplatform.com` host (Microsoft's Q&A
  pages show `*.environment.api.powerplatform.com`), unless the origin is listed in `STUDIO_DELIVERY_ORIGINS`, so a
  connection cannot be pointed at an internal service.
- Email: STARTTLS (default; a server that does not offer it is refused), implicit TLS, or no encryption only to a relay
  on this machine (`localhost` or a loopback address). TLS 1.2 or later with the system's certificate roots. PLAIN
  sign-in only over TLS (or to this machine). Every address is checked again before sending and must be a bare address;
  the subject has its line breaks removed and is Q-encoded; the body is the Markdown as plain UTF-8 text,
  quoted-printable (with a snapshot, `multipart/alternative`: that text plus the designed HTML page, both
  quoted-printable; over 512 KB the HTML part is dropped), with `Auto-Submitted: auto-generated`. Recipients are only in the SMTP envelope (`To:
  undisclosed-recipients:;`), so they do not see each other. The port must be 25, 465, 587 or 2525 unless the host is
  this machine, so an email connection cannot probe other services. An email connection may list `allowed_domains`;
  destinations and sends to other domains are refused.
- "Test" on a slack, teams or email connection sends a short test message (email: to the person testing).

## The designed report (snapshot)

Code: `server/internal/duereport` (`snapshot.go` builds it, `builder.go` reads the tracker, `html.go` and `svg.go` the
page, `text.go` the Markdown and chat summary), `api/reportsnapshots.go`, `api/workorderreports.go`
(`GET /work-orders/{id}/reports/{reportId}/html`), web `features/delivery/duereport/`. Migration
`0020_report_snapshot.sql`.

- When `deliver_report` stores a report, Studio reads the project's tracked items itself through the watch's tracking
  list reader (`watch.TrackingListSource`, the crew's tracking items for that project and their children, at most 2,000)
  with a 20-second limit. On a timeout or error the report is stored and delivered without a snapshot and rendered as
  its Markdown; the delivery entries in the action log carry `details.snapshot`: `included` or `left out: <why>` (a
  fixed sentence, never the tracker's own error text).
- "Today" is the local date in the schedule's time zone (UTC without a schedule). The snapshot holds every open item
  (identifier, title, link, assignee, status and category, due date, labels and blocked, parent and epic, last activity),
  the counts (open, overdue, due today, due within 7 days, blocked, unassigned and due within 14 days, done since the
  previous report), sections (overdue with days late, due today, due in the next 7 days, blocked, unassigned and due
  soon, by person), the next 14 days' due counts, the status mix, and, against the previous report of the same project
  and cadence, deltas and changes (newly overdue, finished, slipped, reassigned, new). Trends come from up to 14 earlier
  snapshots of that project and cadence.
- Size: at most 400 items (items named in a section first), 100 rows per section and 50 people; titles and names are
  cut to 300 characters. Encoded it must fit 512 KB (the column refuses over about 600 KB); past that, items are dropped
  and `truncated` is set. Counts always cover every item.
- The model's Markdown is the narrative. Before it goes anywhere, links whose target is not http or https, and every
  image, keep only their text.
- Exports, all through the delivery paths above and the same dry-run rules:
  - Work order: the Reports tab shows the designed report (counts with deltas and sparklines, the narrative, workload,
    due timeline and status mix charts, sections) with a Text toggle for the Markdown, and "Download HTML".
  - Email: the Markdown built from the data plus the narrative as the text part, and a self-contained HTML page as the
    HTML part.
  - Slack and Teams: a summary (the counts, the five most overdue items, then the narrative), cut to each service's
    limit as before.
  - KM2: the Markdown built from the data plus the narrative.
- The HTML page has inline CSS and inline SVG only: no scripts, no remote assets, a Content-Security-Policy meta tag,
  every tracker string escaped by `html/template` and only http(s) links. The download is sent as an attachment with
  `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox`, `nosniff` and
  `no-store`, to the people who can see the work order's reports (others get 404).
- Charts follow fixed colours: overdue red and due-soon amber (a status pair), an ordinal blue ramp for to do, in
  progress and done, grey for canceled. Every chart has a legend and a table, and the exported page puts the table
  beside the chart, because some mail clients (Gmail among them) drop inline SVG.

## Delivery, logging and retry

- `deliver_report(project, markdown, cadence)` stores the report (with its snapshot) and one delivery row per destination (a snapshot of the
  connection and recipients), keeps the report as the `weekly-report` note of `project:<slug>`, then sends up to four at
  a time. Each send, once started, finishes and is recorded even if the run's tool call times out.
- Each delivery is reserved in the action log as `report:<kind>` (outcome `sending`) with the idempotency key
  `report:<report id>:<position>` before anything is sent, finished with outcome `sent`, and removed when the send fails
  (so it can be retried). A delivery whose key already exists is never sent again; one interrupted mid-send (outcome
  still `sending`) is reported as possibly delivered and not repeated. Delivery is therefore at most once. "Retry failed deliveries" on the
  work order sends only failed deliveries (and ones left pending for over 10 minutes), under a per-report database lock.
- Dry run: no send; each destination is `would_send`, with a dry-run action log entry that never counts as sent.
- Caps: 64 KB per report, 20 reports and 60 sends per run, 10 destinations per schedule, 50 recipients per email
  destination.
- KM2: the report goes through the same save path as `km_save_document` (`api/runkm2_save.go`, `km2SaveDocument`): the
  destination's folder (or the connection's default) among the connection's allowed folders, a `km2:save` action log
  reservation (subject `km2:<external_ref>`, `details.tool = deliver_report`) under the per-run (10) and per-connection
  (200 a day) caps, then KM2's `POST /api/v1/knowledge/documents`. `external_ref` is `<slug>-weekly-<YYYY>-W<ww>`, so a
  resend in the same week is a new version. As for `km_save_document`, the crew must have exactly one KM2 connection. In
  a dry run a dry-run `km2:save` entry is reserved and nothing is sent.
- Tracker: writing status back to Jira or Linear is not built. The `tracker` delivery kind is reserved and, if it ever
  appears, fails with "Sending reports to the tracker is not available yet."
