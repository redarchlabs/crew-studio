-- Work orders, schedules and webhook triggers always run a crew. Each task one of them targeted becomes a crew of
-- its own: the task, every task it takes context from (earlier tasks first) and their agents, which is the crew the
-- server used to build on the fly for a task target. Targets then point at that crew and the task columns go.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION crew_for_task_0010(root uuid) RETURNS uuid AS $$
DECLARE
    root_name   text;
    task_order  uuid[];
    agent_order uuid[];
    crew_inputs jsonb;
    area_list   uuid[];
    crew_name   text;
    suffix      int := 1;
    budget      double precision;
    new_id      uuid;
BEGIN
    SELECT name INTO root_name FROM tasks WHERE id = root;

    -- Furthest from the root first, so every task comes after the tasks whose output it uses
    WITH RECURSIVE chain (id, depth, path) AS (
        SELECT root, 0, ARRAY[root]
        UNION ALL
        SELECT tc.context_task_id, c.depth + 1, c.path || tc.context_task_id
        FROM chain c JOIN task_context tc ON tc.task_id = c.id
        WHERE NOT tc.context_task_id = ANY (c.path)
    )
    SELECT array_agg(x.id ORDER BY x.depth DESC, t.name) INTO task_order
    FROM (SELECT id, max(depth) AS depth FROM chain GROUP BY id) x JOIN tasks t ON t.id = x.id;

    -- Each task's agent, in the order the tasks first use them
    SELECT COALESCE(array_agg(a.agent_id ORDER BY a.first_use), '{}') INTO agent_order
    FROM (SELECT t.agent_id, min(o.ord) AS first_use
          FROM unnest(task_order) WITH ORDINALITY AS o (id, ord) JOIN tasks t ON t.id = o.id
          WHERE t.agent_id IS NOT NULL GROUP BY t.agent_id) a;

    -- One input per {placeholder} in those tasks and agents, in order of first use. A crew that already contains
    -- the task lends its label, default and required flag; otherwise the input is required and labelled by its key.
    WITH texts (ord, sub, body) AS (
        SELECT o.ord, s.sub, s.body
        FROM unnest(task_order) WITH ORDINALITY AS o (id, ord) JOIN tasks t ON t.id = o.id,
             LATERAL (VALUES (1, t.description), (2, t.expected_output)) AS s (sub, body)
        UNION ALL
        SELECT a.first_use, s.sub, s.body
        FROM (SELECT t.agent_id, min(o.ord) AS first_use
              FROM unnest(task_order) WITH ORDINALITY AS o (id, ord) JOIN tasks t ON t.id = o.id
              WHERE t.agent_id IS NOT NULL GROUP BY t.agent_id) a
        JOIN agents g ON g.id = a.agent_id,
             LATERAL (VALUES (3, g.role), (4, g.goal), (5, g.backstory)) AS s (sub, body)
    ), uses AS (
        SELECT m.match[1] AS key, min(ARRAY[x.ord, x.sub, m.pos]) AS first_use
        FROM texts x, LATERAL regexp_matches(x.body, '\{([A-Za-z_][A-Za-z0-9_\-]*)}', 'g') WITH ORDINALITY AS m (match, pos)
        GROUP BY m.match[1]
    ), defined AS (
        SELECT DISTINCT ON (i.value ->> 'key') i.value ->> 'key' AS key, i.value AS input
        FROM crews c JOIN crew_tasks ct ON ct.crew_id = c.id AND ct.task_id = root,
             LATERAL jsonb_array_elements(c.inputs) WITH ORDINALITY AS i (value, ord)
        ORDER BY i.value ->> 'key', c.name, i.ord
    )
    SELECT COALESCE(jsonb_agg(COALESCE(d.input,
               jsonb_build_object('key', u.key, 'label', u.key, 'default', '', 'required', true)) ORDER BY u.first_use), '[]')
    INTO crew_inputs
    FROM uses u LEFT JOIN defined d ON d.key = u.key;

    SELECT COALESCE(array_agg(area_id ORDER BY area_id), '{}') INTO area_list FROM task_areas WHERE task_id = root;
    SELECT default_budget_usd INTO budget FROM settings LIMIT 1;

    -- Named after the task; crew names are unique, so a taken name gets "(task)", then "(task 2)" and so on
    crew_name := left(root_name, 100);
    WHILE EXISTS (SELECT 1 FROM crews WHERE name = crew_name) LOOP
        crew_name := left(root_name, 100) || CASE WHEN suffix = 1 THEN ' (task)' ELSE ' (task ' || suffix || ')' END;
        suffix := suffix + 1;
    END LOOP;

    INSERT INTO crews (name, description, process, inputs, budget_usd)
    VALUES (crew_name, format('Runs the task "%s". Made automatically when work orders, schedules and triggers '
                              'stopped targeting single tasks.', root_name),
            'sequential', crew_inputs, COALESCE(budget, 2.0))
    RETURNING id INTO new_id;
    INSERT INTO crew_tasks (crew_id, task_id, position)
    SELECT new_id, id, ord - 1 FROM unnest(task_order) WITH ORDINALITY AS o (id, ord);
    INSERT INTO crew_agents (crew_id, agent_id, position)
    SELECT new_id, id, ord - 1 FROM unnest(agent_order) WITH ORDINALITY AS o (id, ord);
    INSERT INTO crew_areas (crew_id, area_id) SELECT new_id, unnest(area_list);

    INSERT INTO revisions (entity_type, entity_id, action, snapshot, actor_name, area_ids)
    SELECT 'crew', new_id::text, 'create',
           jsonb_build_object('id', c.id, 'name', c.name, 'description', c.description, 'process', c.process,
                              'manager_llm_id', NULL, 'agent_ids', to_jsonb(agent_order), 'task_ids', to_jsonb(task_order),
                              'inputs', c.inputs, 'budget_usd', c.budget_usd, 'area_ids', to_jsonb(area_list),
                              'created_at', c.created_at, 'updated_at', c.updated_at),
           'Crew Studio upgrade', area_list
    FROM crews c WHERE c.id = new_id;
    RETURN new_id;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    target   uuid;
    new_crew uuid;
BEGIN
    FOR target IN
        SELECT task_id FROM work_orders WHERE target_type = 'task' AND task_id IS NOT NULL
        UNION SELECT task_id FROM schedules WHERE target_type = 'task'
        UNION SELECT task_id FROM triggers WHERE target_type = 'task'
    LOOP
        new_crew := crew_for_task_0010(target);
        UPDATE work_orders SET target_type = 'crew', task_id = NULL, crew_id = new_crew WHERE task_id = target;
        UPDATE schedules SET target_type = 'crew', task_id = NULL, crew_id = new_crew WHERE task_id = target;
        UPDATE triggers SET target_type = 'crew', task_id = NULL, crew_id = new_crew WHERE task_id = target;
    END LOOP;
END;
$$;

DROP FUNCTION crew_for_task_0010(uuid);

-- Work orders whose task was already deleted keep no target, like those whose crew was deleted
ALTER TABLE work_orders DROP COLUMN target_type, DROP COLUMN task_id;
ALTER TABLE schedules DROP COLUMN target_type, DROP COLUMN task_id, ALTER COLUMN crew_id SET NOT NULL;
ALTER TABLE triggers DROP COLUMN target_type, DROP COLUMN task_id, ALTER COLUMN crew_id SET NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Brings the task columns back with every target left on its crew. The crews made from tasks are kept.
ALTER TABLE work_orders
    ADD COLUMN target_type text NOT NULL DEFAULT 'crew' CHECK (target_type IN ('task', 'crew')),
    ADD COLUMN task_id uuid REFERENCES tasks (id) ON DELETE SET NULL;
ALTER TABLE work_orders ALTER COLUMN target_type DROP DEFAULT;
ALTER TABLE schedules
    ALTER COLUMN crew_id DROP NOT NULL,
    ADD COLUMN target_type text NOT NULL DEFAULT 'crew' CHECK (target_type IN ('task', 'crew')),
    ADD COLUMN task_id uuid REFERENCES tasks (id) ON DELETE RESTRICT,
    ADD CHECK ((target_type = 'task') = (task_id IS NOT NULL) AND (target_type = 'crew') = (crew_id IS NOT NULL));
ALTER TABLE schedules ALTER COLUMN target_type DROP DEFAULT;
ALTER TABLE triggers
    ALTER COLUMN crew_id DROP NOT NULL,
    ADD COLUMN target_type text NOT NULL DEFAULT 'crew' CHECK (target_type IN ('task', 'crew')),
    ADD COLUMN task_id uuid REFERENCES tasks (id) ON DELETE RESTRICT,
    ADD CHECK ((target_type = 'task') = (task_id IS NOT NULL) AND (target_type = 'crew') = (crew_id IS NOT NULL));
ALTER TABLE triggers ALTER COLUMN target_type DROP DEFAULT;
-- +goose StatementEnd
