"""Comment and ticket wording the engine picks from (deterministically, via the day's random stream)."""

from __future__ import annotations

START = (
    "Picking this up now.",
    "Starting on this today; I'll push a draft branch by tomorrow.",
    "On it. First pass is going against the sandbox, not the mocks.",
    "Starting this. Shout if the scope in the description is out of date.",
)
TO_REVIEW = (
    "Ready for review, {reviewer}. Test evidence is attached to the PR.",
    "Done from my side; {reviewer} can you review? Edge cases are listed in the PR description.",
    "Moving to review. {reviewer}, the tricky part is the retry path.",
)
DONE = (
    "Reviewed, merged and verified in test.",
    "Merged. Verified with the sandbox data set; closing.",
    "Looks good. Deployed to test and closing this out.",
)
DONE_NON_DEV = (
    "Done; notes are in the project folder.",
    "Closed. Confirmation is attached to this issue.",
    "Completed and shared with the team.",
)
REWORK = (
    "Review feedback: the mapping drops the value when the source field is blank instead of sending null. Back to "
    "In Progress.",
    "Review feedback: needs a test for the duplicate-event case before it can merge.",
    "Review feedback: error messages leak the raw API response; please map them to the catalogue codes.",
    "Review feedback: the transform isn't idempotent on retries yet.",
    "Review feedback: logging is missing the correlation ID on the failure path.",
)
BLOCKED_ON = "Blocked on {blocker} ({title}). {context}"
BLOCKER_CONTEXT = (
    "I need its outcome before I can finish the error handling here.",
    "Can't test this end to end until that lands.",
    "Raised it in stand-up; {owner} knows this is waiting on them.",
    "The sandbox setup it covers is a prerequisite for the next step here.",
    "Parking this for now and helping on that one instead.",
)
UNBLOCK = (
    "Unblocked: {blocker} is done, picking this back up.",
    "Unblocked: found a workaround, we no longer need {blocker} for this.",
    "Unblocked: the other team sorted it this morning.",
    "Unblocked: agreed with {lead} to proceed and revisit after UAT.",
)
SLIP = (
    "Moving the due date to {date}: {reason}.",
    "New target {date}. {reason_cap}.",
    "This won't make the original date; re-planned for {date} ({reason}).",
)
SLIP_REASON = (
    "more edge cases in the sandbox data than expected",
    "review comments took longer than planned",
    "I was pulled onto a production issue for a day",
    "waiting on test data from the business",
    "the ERP API needed a different endpoint than documented",
    "half a day lost to environment problems",
)
REASSIGN = (
    "Reassigning to {new}; {old} is tied up with {other}.",
    "{new} is taking this over so {old} can focus on {other}.",
    "Balancing load for the cycle: moving this to {new}.",
)
CYCLE_PLANNED = "Cycle {number} planned: {planned} items pulled in, {carried} carried over from cycle {previous}."
REPLY = {
    "blocked": (
        "Still blocked on {blocker}. I've chased again and will update here as soon as I hear back.",
        "Yes, still waiting on {blocker}. I'm working on the parts that don't depend on it.",
    ),
    "overdue": (
        "Fair point, this is late. New ETA {eta}; the remaining work is testing.",
        "Sorry, I missed updating this. Should be done by {eta}.",
    ),
    "In Progress": (
        "On track. I expect to move this to review by {eta}.",
        "Making progress; the main flow works, finishing the error handling now.",
    ),
    "In Review": (
        "It's waiting on review from {reviewer}.",
        "In review now; should close this week unless review finds something.",
    ),
    "Todo": (
        "Not started yet; it's next after my current item. Planning to start by {eta}.",
        "Haven't picked this up yet, it's queued behind {other}.",
    ),
    "done": ("This is done; nothing left from my side.", "Closed already, thanks for checking."),
    "other": ("Thanks, noted.", "Ack, I'll take a look."),
}

# Bugs and scope additions that turn up while the project runs: (title, description, area epic key).
NEW_BUGS = (
    ("Account sync fails when the billing country is blank", "The ERP rejects the customer with a 400 on "
     "country code. The CRM allows a blank country on older accounts.", "acct"),
    ("Duplicate contacts created when email differs only by case", "Seen in test: two ERP contacts for the same "
     "person after an email edit in the CRM.", "contact"),
    ("Order line tax off by one cent on multi-line orders", "Rounding per line vs per order. Finance spotted it on "
     "a 14-line test order.", "order"),
    ("Price book sync skips items with a future effective date", "Items priced from next month never appear in the "
     "CRM price book.", "prod"),
    ("Token refresh races under parallel workers", "Two workers refresh at the same time and one request fails "
     "with an invalid token.", "auth"),
    ("Dead-letter replay re-sends already processed messages", "Replay doesn't check the idempotency key before "
     "sending.", "err"),
    ("Invoice balance shows the wrong currency on multi-currency accounts", "Balance uses the account currency "
     "instead of the invoice currency.", "inv"),
    ("Customer number write-back retriggers the account change event", "Write-back causes a second sync of the "
     "same account.", "acct"),
    ("Sales order created twice after a timeout", "The ERP accepted the order but the response timed out; the "
     "retry created a second order.", "order"),
    ("Mapping drops the second street line", "Address line 2 is lost for customers with suite numbers.", "map"),
    ("Pipeline deploys to test without running contract tests", "The deploy job doesn't depend on the contract "
     "stage.", "mw"),
    ("Payment status stuck at 'Partially paid' after a credit memo", "Credit memo application isn't counted in the "
     "balance.", "inv"),
    ("Dashboard lag metric counts weekends", "End-to-end lag spikes every Monday because the batch is paused at "
     "weekends.", "mon"),
    ("Inactive products still selectable on new quotes", "Retired items stay active in the CRM catalogue.", "prod"),
    ("Validation errors missing the record link", "Support can't jump from the error to the source record.", "err"),
    ("Contact opt-out flag not carried on update", "Only set on create; later opt-outs don't reach the ERP.",
     "contact"),
)
NEW_SCOPE = (
    ("Sync shipping addresses as separate ERP ship-to records", "Requested by sales ops: accounts with several "
     "warehouses need one ship-to per address.", "acct"),
    ("Show ERP credit hold status on the CRM account", "Account managers want to see credit holds before quoting.",
     "inv"),
    ("Support quote-level PO numbers on the sales order", "Finance needs the customer PO number on the ERP order.",
     "order"),
    ("Add a sync-status field and error text on CRM records", "So admins can see at a glance which records failed.",
     "err"),
    ("Include product images URL in the catalogue sync", "Marketing request for the quote PDF template.", "prod"),
    ("Weekly reconciliation summary email to finance", "Finance wants the mismatch report as a weekly digest.",
     "mon"),
    ("Runbook entry for vendor sandbox outages", "Agreed in the retro after the sandbox maintenance window.", "cut"),
    ("Sync account owner changes to the ERP sales rep field", "Sales ops wants commissions to follow the CRM "
     "owner.", "acct"),
)
