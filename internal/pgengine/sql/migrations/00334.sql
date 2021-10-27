-- table changes
ALTER TABLE timetable.task
	DROP COLUMN parent_id  CASCADE,
    ADD  COLUMN chain_id   BIGINT REFERENCES timetable.chain(chain_id) ON UPDATE CASCADE ON DELETE CASCADE,
    ADD  COLUMN task_order DOUBLE PRECISION NOT NULL;

COMMENT ON COLUMN timetable."task"."chain_id" IS 'Link to the chain, if NULL task considered to be disabled';
COMMENT ON COLUMN timetable."task"."task_order" IS 'Indicates the order of task within a chain';

ALTER TABLE timetable.parameter
	DROP COLUMN chain_id CASCADE,
	ADD PRIMARY KEY (task_id, order_id);

ALTER TABLE timetable.chain
	DROP COLUMN task_id CASCADE;

-- function changes
CREATE OR REPLACE FUNCTION timetable.delete_task(IN task_id BIGINT) RETURNS boolean AS $$
    WITH del_task AS (DELETE FROM timetable.task WHERE task_id = $1 RETURNING task_id)
    SELECT EXISTS(SELECT 1 FROM del_task)
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.delete_task IS 'Delete the task from a chain';

CREATE OR REPLACE FUNCTION timetable.delete_job(IN job_name TEXT) RETURNS boolean AS $$
    WITH del_chain AS (DELETE FROM timetable.chain WHERE chain.chain_name = $1 RETURNING chain_id)
    SELECT EXISTS(SELECT 1 FROM del_chain)
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.delete_job IS 'Delete the chain and its tasks from the system';

CREATE OR REPLACE FUNCTION timetable.move_task_up(IN task_id BIGINT) RETURNS boolean AS $$
	WITH current_task (ct_chain_id, ct_id, ct_order) AS (
		SELECT chain_id, task_id, task_order FROM timetable.task WHERE task_id = $1
	),
	tasks(t_id, t_new_order) AS (
		SELECT task_id, COALESCE(LAG(task_order) OVER w, LEAD(task_order) OVER w)
		FROM timetable.task t, current_task ct
		WHERE chain_id = ct_chain_id AND (task_order < ct_order OR task_id = ct_id)
		WINDOW w AS (PARTITION BY chain_id ORDER BY ABS(task_order - ct_order))
		LIMIT 2
	),
	upd AS (
		UPDATE timetable.task t SET task_order = t_new_order
		FROM tasks WHERE tasks.t_id = t.task_id AND tasks.t_new_order IS NOT NULL
		RETURNING true
	)
	SELECT COUNT(*) > 0 FROM upd
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.move_task_up IS 'Switch the order of the task execution with a previous task within the chain';

CREATE OR REPLACE FUNCTION timetable.move_task_down(IN task_id BIGINT) RETURNS boolean AS $$
	WITH current_task (ct_chain_id, ct_id, ct_order) AS (
		SELECT chain_id, task_id, task_order FROM timetable.task WHERE task_id = $1
	),
	tasks(t_id, t_new_order) AS (
		SELECT task_id, COALESCE(LAG(task_order) OVER w, LEAD(task_order) OVER w)
		FROM timetable.task t, current_task ct
		WHERE chain_id = ct_chain_id AND (task_order > ct_order OR task_id = ct_id)
		WINDOW w AS (PARTITION BY chain_id ORDER BY ABS(task_order - ct_order))
		LIMIT 2
	),
	upd AS (
		UPDATE timetable.task t SET task_order = t_new_order
		FROM tasks WHERE tasks.t_id = t.task_id AND tasks.t_new_order IS NOT NULL
		RETURNING true
	)
	SELECT COUNT(*) > 0 FROM upd
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.move_task_down IS 'Switch the order of the task execution with a following task within the chain';

DROP FUNCTION IF EXISTS timetable.add_task;
CREATE OR REPLACE FUNCTION timetable.add_task(
    IN kind timetable.command_kind,
    IN command TEXT, 
    IN parent_id BIGINT,
    IN order_delta DOUBLE PRECISION DEFAULT 10
) RETURNS BIGINT AS $$
    INSERT INTO timetable.task (chain_id, task_order, kind, command) 
	SELECT chain_id, task_order + $4, $1, $2 FROM timetable.task WHERE task_id = $3
	RETURNING task_id
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.add_task IS 'Add a task to the same chain as the task with parent_id';

DROP FUNCTION IF EXISTS timetable.add_job;
CREATE OR REPLACE FUNCTION timetable.add_job(
    job_name            TEXT,
    job_schedule        timetable.cron,
    job_command         TEXT,
    job_parameters      JSONB DEFAULT NULL,
    job_kind            timetable.command_kind DEFAULT 'SQL'::timetable.command_kind,
    job_client_name     TEXT DEFAULT NULL,
    job_max_instances   INTEGER DEFAULT NULL,
    job_live            BOOLEAN DEFAULT TRUE,
    job_self_destruct   BOOLEAN DEFAULT FALSE,
    job_ignore_errors   BOOLEAN DEFAULT TRUE,
    job_exclusive       BOOLEAN DEFAULT FALSE
) RETURNS BIGINT AS $$
    WITH 
        cte_chain (v_chain_id) AS (
            INSERT INTO timetable.chain (chain_name, run_at, max_instances, live, self_destruct, client_name, exclusive_execution) 
            VALUES (job_name, job_schedule,job_max_instances, job_live, job_self_destruct, job_client_name, job_exclusive)
            RETURNING chain_id
        ),
        cte_task(v_task_id) AS (
            INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error, autonomous)
            SELECT v_chain_id, 10, job_kind, job_command, job_ignore_errors, TRUE
            FROM cte_chain
            RETURNING task_id
        ),
        cte_param AS (
            INSERT INTO timetable.parameter (task_id, order_id, value)
            SELECT v_task_id, 1, job_parameters FROM cte_task, cte_chain
        )
        SELECT v_chain_id FROM cte_chain
$$ LANGUAGE SQL;

COMMENT ON FUNCTION timetable.add_job IS 'Add one-task chain (aka job) to the system';