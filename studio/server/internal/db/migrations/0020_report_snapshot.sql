-- Designed reports (docs/crew-state-and-pmo-design.md, section 12): when a run delivers a report, Crew Studio reads the
-- project's tracked items itself and keeps a snapshot of them with the report (open items, counts, sections, changes
-- since the previous report, a due timeline and a trend), so the report page, its charts and its exports are built from
-- facts rather than the model's text. The snapshot holds tracker titles and people's names; it lives and is deleted
-- with its report. NULL when the tracker could not be read at delivery.
--
-- The PMO template's weekly task now asks for a narrative around that data instead of re-listing items (copies made by
-- Set up a PMO are not changed).

-- +goose Up
-- +goose StatementBegin
ALTER TABLE reports ADD COLUMN snapshot jsonb CHECK (snapshot IS NULL OR octet_length(snapshot::text) <= 600000);

UPDATE tasks SET description = description || $s$

Crew Studio adds the data to the report itself: when you deliver it, it reads the tracked items and shows the overdue, due-soon, blocked and unassigned work, the changes since the last report and charts. So write the Markdown as the narrative around that data: a short summary with the status, the critical path, the risks and the asks. Do not re-list the items or their dates.$s$
WHERE id = '33333333-0000-4000-8000-000000000202' AND description NOT LIKE '%Crew Studio adds the data to the report itself%';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE tasks SET description = replace(description, $s$

Crew Studio adds the data to the report itself: when you deliver it, it reads the tracked items and shows the overdue, due-soon, blocked and unassigned work, the changes since the last report and charts. So write the Markdown as the narrative around that data: a short summary with the status, the critical path, the risks and the asks. Do not re-list the items or their dates.$s$, '')
WHERE id = '33333333-0000-4000-8000-000000000202';
ALTER TABLE reports DROP COLUMN snapshot;
-- +goose StatementEnd
