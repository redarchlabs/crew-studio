-- Resuming a failed or cancelled crew run (POST /runs/{id}/resume): the new run starts at the first task the earlier
-- run did not finish, and the finished tasks' outputs are copied into it so they are not run (or paid for) again.
--
-- Each task output is now saved as its task finishes, with what it cost, so a run that is killed, times out or stops
-- with the service still keeps the work it finished. A copied output says which run produced it (no foreign key: the
-- copy stays useful when that run is deleted).

-- +goose Up
-- +goose StatementBegin
ALTER TABLE runs
    ADD COLUMN resumed_from_run_id uuid REFERENCES runs (id) ON DELETE SET NULL,
    ADD COLUMN resume_position integer CHECK (resume_position >= 0);

-- Two clicks on Resume must not start two runs that redo the same work
CREATE UNIQUE INDEX runs_one_active_resume ON runs (resumed_from_run_id)
    WHERE resumed_from_run_id IS NOT NULL AND status IN ('queued', 'running');

ALTER TABLE run_task_outputs
    ADD COLUMN reused_from_run_id uuid,
    ADD COLUMN cost_usd double precision CHECK (cost_usd >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE run_task_outputs DROP COLUMN cost_usd, DROP COLUMN reused_from_run_id;
DROP INDEX runs_one_active_resume;
ALTER TABLE runs DROP COLUMN resume_position, DROP COLUMN resumed_from_run_id;
-- +goose StatementEnd
