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
    IN parent_id BIGINT
) RETURNS BIGINT AS $$
    INSERT INTO timetable.task (parent_id, kind, command) VALUES (parent_id, kind, command) RETURNING task_id
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
    job_ignore_errors   BOOLEAN DEFAULT TRUE
) RETURNS BIGINT AS $$
    WITH 
        cte_task(v_task_id) AS (
            INSERT INTO timetable.task (kind, command, ignore_error, autonomous)
            VALUES (job_kind, job_command, job_ignore_errors, TRUE)
            RETURNING task_id
        ),
        cte_chain (v_chain_id) AS (
            INSERT INTO timetable.chain (task_id, chain_name, run_at, max_instances, live,self_destruct, client_name) 
            SELECT v_task_id, job_name, job_schedule,job_max_instances, job_live, job_self_destruct, job_client_name
            FROM cte_task
            RETURNING chain_id
        )
        INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
        SELECT v_chain_id, v_task_id, 1, job_parameters FROM cte_task, cte_chain
        RETURNING chain_id
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
    WITH 
    current_task AS (
        SELECT * FROM timetable.task WHERE task_id = $1), 
    parrent_task AS (
        SELECT t.* FROM timetable.task t, current_task WHERE t.task_id = current_task.parent_id),
    upd_parent AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (ct.task_name, ct.kind, ct.command, ct.run_as, ct.database_connection, ct.ignore_error, ct.autonomous, ct.timeout) 
        FROM current_task ct WHERE t.task_id = ct.parent_id
    ),
    upd_current AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (pt.task_name, pt.kind, pt.command, pt.run_as, pt.database_connection, pt.ignore_error, pt.autonomous, pt.timeout) 
        FROM parrent_task pt WHERE t.task_id = $1
        RETURNING true
    )
    SELECT count(*) > 0 FROM upd_current
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.move_task_down(IN task_id BIGINT) RETURNS boolean AS $$
    WITH 
    current_task AS (
        SELECT * FROM timetable.task WHERE task_id = $1), 
    child_task AS (
        SELECT * FROM timetable.task WHERE parent_id = $1),
    upd_child AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (ct.task_name, ct.kind, ct.command, ct.run_as, ct.database_connection, ct.ignore_error, ct.autonomous, ct.timeout) 
        FROM current_task ct WHERE t.parent_id = $1
    ),
    upd_current AS (
        UPDATE timetable.task t SET 
            (task_name, kind, command, run_as, database_connection, ignore_error, autonomous, timeout) = 
            (pt.task_name, pt.kind, pt.command, pt.run_as, pt.database_connection, pt.ignore_error, pt.autonomous, pt.timeout) 
        FROM child_task pt WHERE t.task_id = $1
        RETURNING true
    )
    SELECT count(*) > 0 FROM upd_current
$$ LANGUAGE SQL;

-- delete_job() will add chain and it's tasks from the system
CREATE OR REPLACE FUNCTION timetable.delete_job(IN job_name TEXT) RETURNS boolean AS $$
    WITH
    del_chain AS (
        DELETE FROM timetable.chain WHERE chain.chain_name = $1 RETURNING task_id),
    del_tasks AS (
		DELETE FROM timetable.task WHERE task.task_id IN (SELECT task_id FROM del_chain)
	)
    SELECT count(*) > 0 FROM del_chain
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.delete_task(IN task_id BIGINT) RETURNS boolean AS $$
    WITH 
    del_task AS (
        DELETE FROM timetable.task WHERE task_id = $1 AND parent_id IS NOT NULL RETURNING parent_id),
    upd_task AS (
        UPDATE timetable.task t SET parent_id = dt.parent_id FROM del_task dt WHERE t.parent_id = $1)
    SELECT count(*) > 0 FROM del_task
$$ LANGUAGE SQL;
