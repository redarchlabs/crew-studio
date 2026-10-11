"""The simulated project as data: people, the ERP project, its cycles, epics and the day-21 seed state.

Everything here is synthetic. The CRM and the ERP are described generically ("the CRM", "the ERP"); people use
``@example.test`` addresses. Days are counted from the project start (day 0 = real today - 21 days at seed time),
so the seed snapshot is "day 21": discovery done, data mapping mostly done, auth in progress and blocked on the
ERP vendor's sandbox credentials, a few items overdue, stale or unassigned.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from .blueprint_tasks import TASKS, TaskSpec

PROJECT_KEY = "ERP"
PROJECT_NAME = "ERP ↔ CRM Integration"
PROJECT_DESCRIPTION = (
    "Bi-directional integration between the CRM (accounts, contacts, opportunities, quotes) and the ERP "
    "(customers, items and price levels, sales orders, invoices and payments) through the middleware layer. "
    "Ten weeks, five two-week cycles: discovery and mapping, connectivity and the middleware foundation, "
    "the sync flows, hardening and monitoring, then UAT, cutover and hypercare."
)
SEED_DAY = 21
CYCLE_COUNT = 5
CYCLE_DAYS = 14
PROJECT_DAYS = CYCLE_COUNT * CYCLE_DAYS
DEFAULT_SEED = 20261005
UMBRELLA_TITLE = "ERP ↔ CRM Integration: program tracking"


@dataclass(frozen=True)
class Person:
    handle: str
    name: str
    email: str
    role: str
    group: str
    capacity: float = 2.5  # story points of progress per working day
    wip: int = 3
    is_bot: bool = False


PEOPLE: tuple[Person, ...] = (
    Person("pm", "Dana Whitfield", "dana.whitfield@example.test", "Delivery lead / project manager", "pm", 1.5, 3),
    Person("arch", "Marcus Okafor", "marcus.okafor@example.test", "Solution architect", "eng", 2.0, 2),
    Person("int1", "Priya Raman", "priya.raman@example.test", "Integration engineer", "eng", 3.0, 3),
    Person("int2", "Tomás Herrera", "tomas.herrera@example.test", "Integration engineer", "eng", 2.5, 2),
    Person("crm", "Lena Fischer", "lena.fischer@example.test", "CRM administrator", "crm", 2.5, 3),
    Person("erp", "Noor Haddad", "noor.haddad@example.test", "ERP business analyst", "erp", 2.0, 2),
    Person("data", "Kenji Watanabe", "kenji.watanabe@example.test", "Data engineer", "data", 2.5, 3),
    Person("qa", "Aisha Bello", "aisha.bello@example.test", "QA engineer", "qa", 3.0, 3),
    Person("bot", "Crew Studio PMO", "pmo-bot@example.test", "PMO assistant (bot)", "bot", 0.0, 0, is_bot=True),
)
PERSON_BY_HANDLE: dict[str, Person] = {p.handle: p for p in PEOPLE}
LEAD = "pm"
BOT = "bot"


@dataclass(frozen=True)
class EpicSpec:
    key: str
    title: str
    owner: str
    group: str
    description: str
    priority: str = "High"


EPICS: tuple[EpicSpec, ...] = (
    EpicSpec("disc", "Discovery and requirements", "pm", "pm",
             "Scope, systems of record, volumes and non-functional requirements, signed off by finance and sales ops."),
    EpicSpec("map", "Data mapping", "erp", "erp",
             "Field-level mapping between CRM objects and ERP records, picklist reconciliation and the external ID "
             "strategy."),
    EpicSpec("auth", "Authentication and connectivity", "int1", "eng",
             "Integration users, OAuth/token auth to both sandboxes, network allow-listing and secrets handling.",
             "Urgent"),
    EpicSpec("mw", "Middleware foundation", "arch", "eng",
             "Environments, CI/CD, queues, canonical message model and the reusable flow template."),
    EpicSpec("acct", "Customer / account sync", "int1", "eng",
             "CRM accounts to ERP customers, with the ERP customer number written back to the CRM."),
    EpicSpec("contact", "Contact sync", "int2", "eng",
             "CRM contacts to ERP customer contacts, including billing-contact rules and opt-out flags.", "Medium"),
    EpicSpec("prod", "Product and price sync", "data", "data",
             "ERP item master and price levels to the CRM product catalogue and price books."),
    EpicSpec("order", "Order sync", "int1", "eng",
             "Closed-won opportunities with approved quotes become ERP sales orders; status flows back to the CRM.",
             "Urgent"),
    EpicSpec("inv", "Invoice and payment back-sync", "int2", "eng",
             "ERP invoices, payments and credit memos summarised on the CRM account for the account team."),
    EpicSpec("err", "Error handling and retries", "int2", "eng",
             "Retry policy, dead-letter replay, validation error catalogue and failure alerting.", "Medium"),
    EpicSpec("mon", "Monitoring and observability", "data", "data",
             "Health dashboard, alert thresholds, reconciliation jobs and audit retention.", "Medium"),
    EpicSpec("uat", "User acceptance testing", "qa", "qa",
             "UAT plan, data, sessions with sales ops and finance, defect triage and sign-off."),
    EpicSpec("cut", "Cutover and hypercare", "pm", "pm",
             "Cutover plan, initial data load, production credentials, dress rehearsal, go-live and hypercare."),
)
EPIC_BY_KEY: dict[str, EpicSpec] = {e.key: e for e in EPICS}
# One area label per epic; tasks carry their epic's label (plus "blocked", "bug" or "scope-change").
EPIC_LABEL: dict[str, str] = {
    "disc": "discovery",
    "map": "data-mapping",
    "auth": "connectivity",
    "mw": "middleware",
    "acct": "crm-to-erp",
    "contact": "crm-to-erp",
    "prod": "erp-to-crm",
    "order": "crm-to-erp",
    "inv": "erp-to-crm",
    "err": "reliability",
    "mon": "reliability",
    "uat": "testing",
    "cut": "cutover",
}


@dataclass(frozen=True)
class SeedComment:
    task: str  # task key, or "" for the umbrella issue
    author: str  # person handle
    day: int
    body: str
    mentions: tuple[str, ...] = field(default_factory=tuple)


SEED_COMMENTS: tuple[SeedComment, ...] = (
    SeedComment("disc.signoff", "pm", 11,
                "Signed off by finance and sales ops in today's steering meeting. The two scope changes (credit memos "
                "in, multi-subsidiary out) are logged in the RAID sheet."),
    SeedComment("map.account", "crm", 12,
                "Reviewed. The CRM 'Account Type' value 'Partner' has no matching ERP customer category; @Noor Haddad "
                "can we add one or map it to 'Reseller'?", ("erp",)),
    SeedComment("map.account", "erp", 12, "Mapping 'Partner' to 'Reseller' for now; finance is fine with that."),
    SeedComment("map.invoice", "erp", 14,
                "Started on invoice status values. Waiting on finance to confirm which payment states the account "
                "team actually needs to see."),
    SeedComment("auth.vendor_creds", "pm", 15,
                "Chased the vendor's partner desk again. The request is with their provisioning team and was promised "
                "by the end of the week."),
    SeedComment("mw.env", "int2", 16,
                "Dev and test environments are up. Test still shares a queue namespace with dev; I'll split them once "
                "the queue task lands."),
    SeedComment("mw.canonical", "arch", 16,
                "Draft account and contact schemas are in the repo. Order schema next, after the order mapping "
                "review."),
    SeedComment("auth.erp_token", "int1", 17,
                "Blocked: the ERP vendor still hasn't issued sandbox API credentials, so I can't get past the token "
                "endpoint. I've stubbed the auth client against the published API reference in the meantime."),
    SeedComment("auth.network", "arch", 18,
                "Raised the allow-list request with the ERP hosting team. They need our static egress IPs, which come "
                "with the middleware networking change."),
    SeedComment("auth.vendor_creds", "pm", 19,
                "Still no credentials. Escalated to our account manager at the vendor; @Priya Raman keep going on the "
                "stubbed client.", ("int1",)),
    SeedComment("map.order", "erp", 19,
                "Ready for review. Open question: quote line discounts vs header discounts. @Marcus Okafor your call.",
                ("arch",)),
    SeedComment("map.order", "arch", 20,
                "Reviewing today. Header discounts should become a separate discount line on the sales order; notes "
                "inline in the mapping sheet."),
    SeedComment("", "pm", 14,
                "Cycle 2 planned: connectivity, middleware foundation and the rest of the mapping. Discovery is "
                "closed."),
)

__all__ = [
    "BOT",
    "CYCLE_COUNT",
    "CYCLE_DAYS",
    "DEFAULT_SEED",
    "EPICS",
    "EPIC_BY_KEY",
    "EPIC_LABEL",
    "LEAD",
    "PEOPLE",
    "PERSON_BY_HANDLE",
    "PROJECT_DAYS",
    "PROJECT_DESCRIPTION",
    "PROJECT_KEY",
    "PROJECT_NAME",
    "SEED_COMMENTS",
    "SEED_DAY",
    "TASKS",
    "UMBRELLA_TITLE",
    "EpicSpec",
    "Person",
    "SeedComment",
    "TaskSpec",
]
