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
$$ LANGUAGE SQL STRICT;

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

