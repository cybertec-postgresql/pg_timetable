CREATE OR REPLACE FUNCTION timetable.get_chain_running_statuses(chain_id BIGINT) RETURNS SETOF BIGINT AS $$
    SELECT  start_status.run_status_id 
    FROM    timetable.run_status start_status
    WHERE   start_status.execution_status = 'CHAIN_STARTED' 
            AND start_status.chain_id = $1
            AND NOT EXISTS (
                SELECT 1
                FROM    timetable.run_status finish_status
                WHERE   start_status.run_status_id = finish_status.start_status_id
                        AND finish_status.execution_status IN ('CHAIN_FAILED', 'CHAIN_DONE', 'DEAD')
            )
    ORDER BY 1
$$ LANGUAGE SQL STRICT;

COMMENT ON FUNCTION timetable.get_chain_running_statuses(chain_id BIGINT) IS
    'Returns a set of active run status IDs for a given chain';

CREATE OR REPLACE FUNCTION timetable.health_check(client_name TEXT) RETURNS void AS $$
    INSERT INTO timetable.run_status
        (execution_status, start_status_id, client_name)
    SELECT 
        'DEAD', start_status.run_status_id, $1 
        FROM    timetable.run_status start_status
        WHERE   start_status.execution_status = 'CHAIN_STARTED' 
            AND start_status.client_name = $1
            AND NOT EXISTS (
                SELECT 1
                FROM    timetable.run_status finish_status
                WHERE   start_status.run_status_id = finish_status.start_status_id
                        AND finish_status.execution_status IN ('CHAIN_FAILED', 'CHAIN_DONE', 'DEAD')
            )
$$ LANGUAGE SQL STRICT; 

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

-- add_job() will add one-task chain to the system
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

CREATE OR REPLACE FUNCTION timetable.notify_chain_start(
    chain_id BIGINT, 
    worker_name TEXT
) RETURNS void AS $$
    SELECT pg_notify(
        worker_name, 
        format('{"ConfigID": %s, "Command": "START", "Ts": %s}', 
        chain_id, 
        EXTRACT(epoch FROM clock_timestamp())::bigint)
    )
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_stop(
    chain_id BIGINT, 
    worker_name TEXT
) RETURNS void AS  $$ 
    SELECT pg_notify(
        worker_name, 
        format('{"ConfigID": %s, "Command": "STOP", "Ts": %s}', 
            chain_id, 
            EXTRACT(epoch FROM clock_timestamp())::bigint)
        )
$$ LANGUAGE SQL;

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

-- delete_job() will delete chain and it's tasks from the system
CREATE OR REPLACE FUNCTION timetable.delete_job(IN job_name TEXT) RETURNS boolean AS $$
    WITH del_chain AS (DELETE FROM timetable.chain WHERE chain.chain_name = $1 RETURNING chain_id)
    SELECT EXISTS(SELECT 1 FROM del_chain)
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.delete_task(IN task_id BIGINT) RETURNS boolean AS $$
    WITH del_task AS (DELETE FROM timetable.task WHERE task_id = $1 RETURNING task_id)
    SELECT EXISTS(SELECT 1 FROM del_task)
$$ LANGUAGE SQL;
