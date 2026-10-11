-- Crew state, phase 4 (docs/crew-state-and-pmo-design.md, sections 10, 12 and 16): Jira and Linear trackers.
-- A crew's tracking list (the tracker items it follows, with their child tickets), a people map per tracker
-- connection (tracker accounts linked to Studio users), and safety caps on a watch for how often the crew may
-- mention one person and how many comments one run may post. No permissions are added: the tracking list uses
-- state.view and state.manage, the people map connections.view and connections.manage.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE tracking_items (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id          uuid NOT NULL REFERENCES crews (id) ON DELETE CASCADE,
    -- NULL: the crew's whole tracking list; otherwise only that schedule's tracking_list watch reads it
    schedule_id      uuid REFERENCES schedules (id) ON DELETE CASCADE,
    connection_id    uuid NOT NULL REFERENCES connections (id) ON DELETE RESTRICT,
    project          text NOT NULL,
    -- The tracker's key for the item, such as PRJ-10 (Jira) or ENG-42 (Linear)
    item_ref         text NOT NULL,
    include_children text NOT NULL DEFAULT 'all' CHECK (include_children IN ('all', 'direct', 'none')),
    note             text NOT NULL DEFAULT '',
    -- Who escalations go to instead of the tracker's project lead (a tracker account id); '' for the project lead
    lead_account_id  text NOT NULL DEFAULT '',
    added_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    added_at         timestamptz NOT NULL DEFAULT now(),
    -- Kept for history; NULL while tracked
    removed_at       timestamptz,
    removed_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    UNIQUE NULLS NOT DISTINCT (crew_id, schedule_id, connection_id, item_ref, removed_at)
);
CREATE INDEX tracking_items_crew_idx ON tracking_items (crew_id) WHERE removed_at IS NULL;
CREATE INDEX tracking_items_connection_idx ON tracking_items (connection_id);

-- Tracker accounts seen through a connection, linked to Studio users by email where the tracker shows it, or by
-- hand. Used to name people consistently and to know whom a follow-up may mention.
CREATE TABLE tracker_people (
    connection_id uuid NOT NULL REFERENCES connections (id) ON DELETE CASCADE,
    account_id    text NOT NULL,
    display_name  text NOT NULL DEFAULT '',
    email         text NOT NULL DEFAULT '',
    user_id       uuid REFERENCES users (id) ON DELETE SET NULL,
    -- 'email' when matched automatically, 'manual' when a person chose (or cleared) the link, '' when never linked
    link          text NOT NULL DEFAULT '' CHECK (link IN ('', 'email', 'manual')),
    seen_at       timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, account_id)
);
CREATE INDEX tracker_people_user_idx ON tracker_people (user_id) WHERE user_id IS NOT NULL;

-- Safety caps (section 16, item 8), checked in code by the tracker tools
ALTER TABLE watches
    ADD COLUMN mention_cap_per_day integer NOT NULL DEFAULT 3 CHECK (mention_cap_per_day BETWEEN 1 AND 50),
    ADD COLUMN comment_cap_per_run integer NOT NULL DEFAULT 30 CHECK (comment_cap_per_run BETWEEN 1 AND 500);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Deletes every crew's tracking list and every connection's people map.
ALTER TABLE watches DROP COLUMN comment_cap_per_run, DROP COLUMN mention_cap_per_day;
DROP TABLE tracker_people, tracking_items;
-- +goose StatementEnd
