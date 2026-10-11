"""The ~85 tasks of the ERP <-> CRM Integration project and their day-21 seed state.

``deps`` become ``blocks`` links (dependency -> task) and gate when work can start. ``blocked_by`` is a raised
blocker at seed time (``blocked`` label plus an active ``blocks`` link). Days count from the project start; a
task's due date defaults to the end of its planned cycle.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class TaskSpec:
    key: str
    epic: str
    title: str
    issue_type: str
    estimate: int
    owner: str
    cycle: int
    state: str  # done | review | prog | todo | backlog
    description: str
    deps: tuple[str, ...] = ()
    due: int | None = None
    start: int | None = None
    done: int | None = None
    activity: int | None = None
    unassigned: bool = False
    priority: str = "Medium"
    blocked_by: tuple[str, ...] = ()


def t(key: str, title: str, issue_type: str, estimate: int, owner: str, cycle: int, state: str, description: str,
      **kw: object) -> TaskSpec:
    return TaskSpec(key=key, epic=key.split(".", 1)[0], title=title, issue_type=issue_type, estimate=estimate,
                    owner=owner, cycle=cycle, state=state, description=description, **kw)  # type: ignore[arg-type]


TASKS: tuple[TaskSpec, ...] = (
    # ---------------------------------------------------------------- discovery (cycle 1, done)
    t("disc.kickoff", "Run integration kickoff and confirm scope with business owners", "Task", 2, "pm", 1, "done",
      "Kickoff with sales ops, finance and IT. Confirm in-scope objects, directions of sync and the go-live window; "
      "capture open questions in the RAID log.", done=2, priority="High"),
    t("disc.systems", "Inventory CRM objects and ERP records in scope", "Task", 3, "arch", 1, "done",
      "List every CRM object (standard and custom) and ERP record type that the flows touch, with owners, record "
      "counts and API availability on both sides.", done=4, deps=("disc.kickoff",)),
    t("disc.volumes", "Capture data volumes and peak sync windows", "Task", 2, "data", 1, "done",
      "Daily create/update volumes per object, month-end and quarter-end peaks, and the ERP batch windows we must "
      "avoid.", done=5, deps=("disc.kickoff",)),
    t("disc.sor", "Agree system of record per entity", "Story", 3, "arch", 1, "done",
      "For account, contact, product, price, order, invoice and payment: which system owns create, which owns "
      "update, and what happens on conflict.", done=7, deps=("disc.systems",), priority="High"),
    t("disc.nfr", "Document non-functional requirements", "Task", 2, "arch", 1, "done",
      "Latency targets per flow, retry and replay expectations, audit and retention needs, and the support model "
      "after go-live.", done=8, deps=("disc.systems",)),
    t("disc.risks", "Build the RAID log and integration risk register", "Task", 1, "pm", 1, "done",
      "Risks, assumptions, issues and dependencies with owners. Vendor sandbox access is the top risk.", done=6),
    t("disc.signoff", "Requirements sign-off with finance and sales ops", "Task", 1, "pm", 1, "done",
      "Walk through the scope, system-of-record matrix and NFRs; record the sign-off and any scope changes.",
      done=11, deps=("disc.sor", "disc.nfr"), priority="High"),
    # ---------------------------------------------------------------- data mapping (cycles 1-2, mostly done)
    t("map.ids", "Define the external ID strategy and cross-reference table", "Story", 3, "arch", 1, "done",
      "Which side stores which key, how the middleware cross-reference table is keyed, and how merges and "
      "re-parenting are handled.", done=10, deps=("disc.sor",), priority="High"),
    t("map.account", "Map CRM Account fields to the ERP customer master", "Story", 5, "erp", 1, "done",
      "Field-by-field mapping including billing/shipping addresses, payment terms, credit limit, tax registration "
      "and customer category.", done=12, deps=("disc.sor",), priority="High"),
    t("map.contact", "Map CRM Contact to ERP customer contacts", "Task", 3, "crm", 1, "done",
      "Names, emails, phones, roles and the primary billing contact flag; confirm which contacts the ERP actually "
      "needs.", done=13, deps=("disc.sor",)),
    t("map.product", "Map the ERP item master and price levels to CRM products and price books", "Story", 5, "erp",
      2, "done", "Item types in scope, units of measure, active flags, currencies and how ERP price levels become CRM "
      "price book entries.", done=16, deps=("disc.sor",)),
    t("map.uom", "Map units of measure and pack sizes", "Task", 2, "erp", 2, "done",
      "ERP UOM codes and pack conversions to the CRM quantity model; flag items sold in more than one unit.",
      done=15, deps=("map.product",)),
    t("map.picklists", "Reconcile picklist values (payment terms, regions, tax codes)", "Task", 3, "crm", 2, "done",
      "Side-by-side list of every picklist used by the mapped fields, with the agreed translation table.",
      done=17, deps=("map.account",)),
    t("map.order", "Map CRM opportunity and quote to the ERP sales order", "Story", 5, "erp", 2, "review",
      "Header and line mapping from the approved quote: pricing, discounts, tax, shipping method, requested date "
      "and the opportunity reference on the order.", deps=("map.account", "map.product"), start=15, activity=20,
      due=22, priority="High"),
    t("map.invoice", "Map ERP invoice and payment status back to the CRM", "Task", 3, "erp", 2, "prog",
      "Invoice header fields, open balance, due date, payment status values and credit memos for the CRM account "
      "summary.", deps=("map.account",), start=14, activity=14, due=19),
    t("map.review", "Mapping walkthrough with finance", "Task", 1, "pm", 2, "todo",
      "Walk finance through the order and invoice mappings and get written confirmation of the tax and rounding "
      "rules.", deps=("map.order", "map.invoice")),
    # ---------------------------------------------------------------- auth & connectivity (cycle 2)
    t("auth.vendor_creds", "Obtain ERP vendor sandbox credentials and API user", "Task", 1, "pm", 2, "prog",
      "Request the sandbox tenant, an integration role with the right permissions and the API credentials from "
      "the ERP vendor. Escalation path: vendor account manager.", start=10, activity=15, due=18, priority="Urgent"),
    t("auth.crm_app", "Register the connected app / OAuth client in the CRM sandbox", "Task", 2, "crm", 2, "done",
      "Create the integration user and permission set, register the OAuth client with the JWT bearer certificate "
      "and restrict it to the integration profile.", done=15),
    t("auth.erp_token", "Implement token-based auth to the ERP sandbox", "Story", 5, "int1", 2, "prog",
      "Token-based authentication client for the ERP REST API with signing, clock-skew handling and token reuse "
      "across workers.", start=16, activity=17, priority="Urgent", blocked_by=("auth.vendor_creds",)),
    t("auth.crm_oauth", "Implement the OAuth JWT bearer flow to the CRM", "Story", 3, "int2", 2, "prog",
      "Server-to-server JWT bearer flow with certificate rotation support and a cached access token.",
      deps=("auth.crm_app",), start=17, activity=20, priority="High"),
    t("auth.network", "Allow-list middleware egress IPs with ERP hosting", "Task", 2, "arch", 2, "prog",
      "Static egress IPs for the middleware runtime, firewall request to the ERP hosting team, and a test from "
      "each environment.", start=18, activity=18, due=20),
    t("auth.secrets", "Store integration credentials in the secrets vault and plan rotation", "Task", 2, "int2", 2,
      "todo", "Vault paths per environment, access policies for the runtime identity, and a documented rotation "
      "procedure for both systems."),
    t("auth.ratelimits", "Confirm ERP API rate limits and concurrency caps", "Task", 1, "arch", 2, "todo",
      "Get the documented and the actual limits for the sandbox and production tenants, including concurrent "
      "request caps per integration user."),
    t("auth.smoke", "Connectivity smoke test in both directions", "Task", 1, "qa", 2, "todo",
      "Authenticated read and write against both sandboxes from the middleware test environment, scripted so it "
      "can run after every deployment.", deps=("auth.erp_token", "auth.crm_oauth"), unassigned=True),
    # ---------------------------------------------------------------- middleware foundation (cycles 2-3)
    t("mw.env", "Provision middleware dev and test environments", "Task", 3, "int2", 2, "done",
      "Runtime, networking, storage and access for dev and test; infrastructure as code in the integration repo.",
      done=16, priority="High"),
    t("mw.pipeline", "CI/CD pipeline for integration flows", "Task", 3, "int2", 2, "prog",
      "Build, unit test, package and deploy flows to dev on merge and to test on tag, with environment-specific "
      "configuration.", deps=("mw.env",), start=18, activity=20),
    t("mw.queue", "Set up the message queue and dead-letter queue", "Story", 3, "int1", 2, "todo",
      "Queues per flow and direction, dead-letter queues with retention, and the namespace split between dev and "
      "test.", deps=("mw.env",), priority="High"),
    t("mw.canonical", "Define the canonical message model (JSON schemas)", "Story", 5, "arch", 2, "prog",
      "Versioned JSON schemas for account, contact, product, order and invoice messages, with the external IDs "
      "from the cross-reference design.", deps=("map.ids",), start=15, activity=16, priority="High"),
    t("mw.config", "Environment configuration and feature flags per flow", "Task", 2, "int1", 2, "todo",
      "Per-environment endpoints and switches so each flow can be enabled, paused or put in read-only mode "
      "without a deployment.", unassigned=True),
    t("mw.logging", "Structured logging and correlation IDs", "Task", 2, "int2", 3, "backlog",
      "One correlation ID from the source event to the target write, structured log fields and log retention per "
      "environment.", deps=("mw.pipeline",)),
    t("mw.framework", "Reusable flow template with idempotency keys", "Story", 5, "int1", 3, "backlog",
      "Template for extract, transform, load and acknowledge with idempotency keys, retries and metrics built in.",
      deps=("mw.canonical", "mw.queue"), priority="High"),
    t("mw.contract", "Contract tests between the middleware and both APIs", "Task", 3, "qa", 3, "backlog",
      "Consumer-driven contract tests against recorded sandbox responses, run in the pipeline.",
      deps=("mw.pipeline",)),
    # ---------------------------------------------------------------- customer / account sync (cycle 3)
    t("acct.extract", "CRM Account change capture", "Story", 5, "int1", 3, "backlog",
      "Subscribe to account change events in the CRM, filter to in-scope record types and publish canonical "
      "messages.", deps=("auth.crm_oauth", "mw.framework"), priority="High"),
    t("acct.transform", "Account to customer transform and validation", "Task", 3, "int2", 3, "backlog",
      "Apply the account mapping, validate required ERP fields and route invalid records to the validation queue.",
      deps=("map.account", "mw.canonical")),
    t("acct.address", "Address normalisation and country codes", "Task", 2, "data", 3, "backlog",
      "Normalise country and state values to ISO codes and split street lines the way the ERP expects."),
    t("acct.upsert", "Upsert the customer in the ERP by external ID", "Story", 5, "int1", 3, "backlog",
      "Create or update the ERP customer using the cross-reference table; handle concurrent updates and partial "
      "failures.", deps=("auth.erp_token", "acct.transform"), priority="High"),
    t("acct.writeback", "Write the ERP customer number back to the CRM", "Task", 2, "int2", 3, "backlog",
      "Store the ERP customer number and sync status on the CRM account without retriggering the change event.",
      deps=("acct.upsert",)),
    t("acct.dedupe", "Duplicate account detection before create", "Task", 3, "data", 3, "backlog",
      "Match rules (tax ID, normalised name plus postcode) to stop the sync creating duplicate ERP customers.",
      deps=("map.account",), unassigned=True),
    t("acct.test", "Account sync test scenarios (create, update, merge)", "Task", 3, "qa", 3, "backlog",
      "Scripted scenarios including merges and re-parenting, run against both sandboxes.",
      deps=("acct.writeback",)),
    t("acct.hierarchy", "Parent/child account hierarchy handling", "Story", 3, "int2", 4, "backlog",
      "Sync parent accounts first, map the hierarchy to ERP bill-to/ship-to relationships.", deps=("acct.upsert",)),
    # ---------------------------------------------------------------- contact sync (cycles 3-4)
    t("contact.flow", "Contact sync flow, CRM to ERP", "Story", 5, "int2", 3, "backlog",
      "Contacts of synced accounts to ERP customer contacts, keyed by the CRM contact ID.",
      deps=("acct.upsert", "map.contact")),
    t("contact.primary", "Primary billing contact rules", "Task", 2, "crm", 3, "backlog",
      "Exactly one primary billing contact per customer; define what happens when the CRM has none or several.",
      deps=("contact.flow",)),
    t("contact.optout", "Respect email opt-out and privacy flags in the sync", "Task", 2, "crm", 3, "backlog",
      "Carry opt-out and do-not-contact flags to the ERP and exclude contacts flagged for erasure.",
      unassigned=True),
    t("contact.inactive", "Handle deactivated contacts", "Task", 2, "int2", 4, "backlog",
      "Deactivate rather than delete in the ERP; keep history on invoices that reference the contact.",
      deps=("contact.flow",)),
    t("contact.test", "Contact sync regression tests", "Task", 2, "qa", 4, "backlog",
      "Regression pack for create, update, deactivate and primary-contact changes.", deps=("contact.flow",)),
    # ---------------------------------------------------------------- product & price sync (cycles 3-4)
    t("prod.extract", "Item master extract from the ERP (delta by last modified)", "Story", 3, "data", 3, "backlog",
      "Delta extract of sellable items with a watermark, including inactive items for retirement.",
      deps=("auth.erp_token", "map.product")),
    t("prod.load", "Load products into the CRM product catalogue", "Story", 3, "int1", 3, "backlog",
      "Upsert products by item number, keep family and UOM in step, never delete products on open quotes.",
      deps=("prod.extract",)),
    t("prod.price", "Price book sync with currencies and price levels", "Story", 5, "data", 4, "backlog",
      "ERP price levels to CRM price books per currency, with effective dates.", deps=("prod.load",),
      priority="High"),
    t("prod.retire", "Retire discontinued items without breaking open quotes", "Task", 2, "crm", 4, "backlog",
      "Deactivate retired products and warn the quote owner instead of failing the quote.", deps=("prod.load",)),
    t("prod.schedule", "Nightly schedule and runtime monitoring for the catalogue sync", "Task", 1, "int1", 4,
      "backlog", "Schedule outside the ERP batch window and alert when the run overruns.", deps=("prod.load",)),
    t("prod.test", "Product and price reconciliation report", "Task", 3, "qa", 4, "backlog",
      "Compare item and price counts and sample values between both systems after each run.",
      deps=("prod.price",)),
    # ---------------------------------------------------------------- order sync (cycles 4-5)
    t("order.trigger", "Trigger order creation on closed-won with an approved quote", "Story", 3, "crm", 4,
      "backlog", "Validation rules and the platform event that fires when an opportunity closes with an approved "
      "primary quote.", deps=("map.order",), priority="High"),
    t("order.create", "Create the sales order in the ERP with lines, tax and shipping", "Story", 8, "int1", 4,
      "backlog", "Build the sales order from the quote, resolve customer and items by external ID, apply tax codes "
      "and shipping.", deps=("order.trigger", "acct.upsert", "prod.load"), priority="Urgent"),
    t("order.status", "Sync order status and fulfilment back to the CRM", "Story", 5, "int2", 4, "backlog",
      "Order status, shipment and tracking updates on the opportunity and a related order record.",
      deps=("order.create",)),
    t("order.amend", "Handle order amendments and cancellations", "Story", 5, "int1", 5, "backlog",
      "Quote revisions after order creation, line cancellations and full cancellations, with ERP-side locks.",
      deps=("order.create",)),
    t("order.tax", "Tax code and discount mapping validation", "Task", 3, "erp", 4, "backlog",
      "Validate tax codes and the header-discount line approach on real quotes from each region.",
      deps=("order.create",)),
    t("order.test", "End-to-end order scenarios including multi-currency", "Task", 5, "qa", 5, "backlog",
      "Quote to order to fulfilment scenarios across currencies, discounts and partial shipments.",
      deps=("order.status",), priority="High"),
    # ---------------------------------------------------------------- invoice & payment back-sync (cycles 4-5)
    t("inv.extract", "Invoice extract and posting events from the ERP", "Story", 3, "data", 4, "backlog",
      "Posted invoices and payment applications since the last watermark, published as canonical messages.",
      deps=("map.invoice", "auth.erp_token")),
    t("inv.crm", "Invoice summary object and related list on the CRM account", "Task", 3, "crm", 4, "backlog",
      "Custom object, page layout and related list so account managers see open invoices and balances.",
      deps=("map.invoice",)),
    t("inv.payment", "Payment status and balance due sync", "Story", 5, "int2", 4, "backlog",
      "Update invoice payment status and the account balance due from ERP payment applications.",
      deps=("inv.extract", "inv.crm"), priority="High"),
    t("inv.credit", "Credit memo handling", "Task", 3, "int2", 5, "backlog",
      "Show credit memos against the original invoice and adjust the open balance.", deps=("inv.payment",)),
    t("inv.ar", "AR ageing snapshot for account managers", "Task", 2, "data", 5, "backlog",
      "Weekly ageing buckets per account from the synced invoices.", deps=("inv.payment",), priority="Low"),
    t("inv.test", "Invoice and payment reconciliation with finance", "Task", 3, "qa", 5, "backlog",
      "Reconcile a month of invoices and payments with finance's ledger extract.", deps=("inv.payment",)),
    # ---------------------------------------------------------------- error handling (cycles 3-4)
    t("err.retry", "Retry policy with exponential backoff per endpoint", "Story", 3, "int1", 3, "backlog",
      "Retry transient failures with backoff and jitter, honour rate-limit responses, and stop on validation "
      "errors.", deps=("mw.queue",)),
    t("err.validation", "Validation error catalogue and user-facing messages", "Task", 3, "arch", 3, "backlog",
      "Every validation failure gets a code, an owner and a message a sales ops admin can act on."),
    t("err.dlq", "Dead-letter replay tool for support", "Story", 5, "int2", 4, "backlog",
      "Inspect, fix and replay dead-lettered messages with an audit trail.", deps=("err.retry",)),
    t("err.notify", "Alert routing for failed syncs to the support queue", "Task", 2, "int1", 4, "backlog",
      "Create a support ticket for repeated failures on the same record instead of alerting per message.",
      deps=("err.dlq",)),
    t("err.idempotency", "Idempotency tests for duplicate events", "Task", 3, "qa", 4, "backlog",
      "Replay the same event several times and check that no duplicate customers, orders or payments appear.",
      deps=("mw.framework",)),
    # ---------------------------------------------------------------- monitoring (cycles 4-5)
    t("mon.dashboard", "Integration health dashboard (throughput, lag, errors)", "Story", 3, "data", 4, "backlog",
      "Per-flow throughput, end-to-end lag and error rates, with drill-down by correlation ID.",
      deps=("mw.logging",)),
    t("mon.alerts", "Alert thresholds and on-call runbook", "Task", 2, "arch", 4, "backlog",
      "Thresholds per flow and the runbook entries support will follow at go-live.", deps=("mon.dashboard",)),
    t("mon.recon", "Daily reconciliation job: CRM vs ERP record counts", "Story", 5, "data", 4, "backlog",
      "Nightly counts and checksums of synced records with a report of mismatches.", deps=("acct.upsert",)),
    t("mon.audit", "Audit trail retention for financial sync events", "Task", 2, "arch", 5, "backlog",
      "Retention and access rules for order, invoice and payment sync logs agreed with finance and audit.",
      priority="Low"),
    # ---------------------------------------------------------------- UAT (cycles 4-5)
    t("uat.plan", "UAT plan, scripts and sign-off criteria", "Task", 3, "qa", 4, "backlog",
      "Scenarios per business role, entry and exit criteria, and the defect severity scale."),
    t("uat.data", "Prepare the UAT data set in both sandboxes", "Task", 3, "data", 5, "backlog",
      "Representative accounts, products, open quotes and invoices, refreshed before each UAT round.",
      deps=("uat.plan",)),
    t("uat.sales", "UAT with sales ops (accounts, contacts, orders)", "Task", 3, "qa", 5, "backlog",
      "Facilitated sessions with sales ops on the account, contact and order flows.",
      deps=("uat.data", "order.status"), priority="High"),
    t("uat.finance", "UAT with finance (invoices, payments, credit memos)", "Task", 3, "qa", 5, "backlog",
      "Facilitated sessions with finance on the invoice and payment back-sync.",
      deps=("uat.data", "inv.payment"), priority="High"),
    t("uat.defects", "UAT defect triage and fixes", "Task", 5, "int1", 5, "backlog",
      "Daily triage during UAT; fix, redeploy and re-test.", deps=("uat.sales",)),
    t("uat.signoff", "UAT sign-off", "Task", 1, "pm", 5, "backlog",
      "Written sign-off from sales ops and finance against the exit criteria.",
      deps=("uat.sales", "uat.finance"), priority="High"),
    # ---------------------------------------------------------------- cutover & hypercare (cycles 4-5)
    t("cut.plan", "Cutover plan and rollback criteria", "Task", 3, "pm", 4, "backlog",
      "Hour-by-hour cutover plan, go/no-go criteria, rollback steps and communications."),
    t("cut.migration", "Initial load of historical accounts and open orders", "Story", 5, "data", 5, "backlog",
      "Bulk load existing customers and open orders through the same transforms, with a reconciliation report.",
      deps=("acct.upsert", "cut.plan")),
    t("cut.prodcreds", "Production credentials and connected app in production", "Task", 2, "crm", 5, "backlog",
      "Integration users, OAuth client and ERP API credentials in production, stored in the vault.",
      deps=("cut.plan",), priority="High"),
    t("cut.dryrun", "Cutover dress rehearsal in staging", "Task", 3, "int2", 5, "backlog",
      "Full rehearsal of the cutover plan against a production-sized copy, timed.", deps=("cut.migration",)),
    t("cut.training", "Train sales ops and finance on the new sync behaviour", "Task", 2, "crm", 5, "backlog",
      "Short sessions and a one-page guide: what syncs, when, and how to report a sync problem."),
    t("cut.golive", "Go-live: enable flows and freeze manual entry", "Task", 2, "pm", 5, "backlog",
      "Execute the cutover plan, enable flows in production and stop manual double entry.",
      deps=("cut.dryrun", "uat.signoff"), priority="Urgent"),
    t("cut.hypercare", "Two-week hypercare rota and daily triage", "Task", 3, "int1", 5, "backlog",
      "Named owners per day, a daily triage call and a burn-down of post-go-live issues.", deps=("cut.golive",)),
)
