-- Report cadence (docs/crew-state-and-pmo-design.md, section 12): besides the weekly status report, a crew's daily run
-- may deliver a daily report. cadence says which; period is what the report covers: the ISO week of a weekly report
-- (2026-W41) or the local date of a daily one (2026-10-06), in the schedule's time zone. week stays the ISO week the
-- report was made in. A run may deliver one report of each cadence per project.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE reports ADD COLUMN cadence text NOT NULL DEFAULT 'weekly' CHECK (cadence IN ('weekly', 'daily'));
ALTER TABLE reports ADD COLUMN period text;
UPDATE reports SET period = week;
ALTER TABLE reports ALTER COLUMN period SET NOT NULL;
ALTER TABLE reports DROP CONSTRAINT reports_run_id_project_slug_key;
ALTER TABLE reports ADD CONSTRAINT reports_run_id_project_slug_cadence_key UNIQUE (run_id, project_slug, cadence);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes the daily reports (and their deliveries; their action log entries and notes stay), which the weekly-only
-- schema cannot hold next to a weekly report of the same run and project
DELETE FROM reports WHERE cadence = 'daily';
ALTER TABLE reports DROP CONSTRAINT reports_run_id_project_slug_cadence_key;
ALTER TABLE reports ADD CONSTRAINT reports_run_id_project_slug_key UNIQUE (run_id, project_slug);
ALTER TABLE reports DROP COLUMN period;
ALTER TABLE reports DROP COLUMN cadence;
-- +goose StatementEnd
