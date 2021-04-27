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
    IN command_name TEXT, 
    IN parent_task_id BIGINT
) RETURNS BIGINT AS $$
DECLARE
    v_command_id BIGINT;
    v_result_id BIGINT;
BEGIN
    SELECT command_id FROM timetable.command WHERE name = command_name INTO v_command_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Nonexistent command --> %', command_name
        USING 
            ERRCODE = 'invalid_parameter_value',
            HINT = 'Please check your command name parameter';
    END IF;
    INSERT INTO timetable.task 
        (task_id, parent_id, command_id)
    VALUES 
        (DEFAULT, parent_task_id, v_command_id)
    RETURNING task_id INTO v_result_id;
    RETURN v_result_id;
END
$$ LANGUAGE PLPGSQL;

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
        cte_cmd(v_command_id) AS (
            INSERT INTO timetable.command (command_id, name, kind, script)
            VALUES (DEFAULT, job_name, job_kind, job_command)
            RETURNING command_id
        ),
        cte_task(v_task_id) AS (
            INSERT INTO timetable.task (command_id, ignore_error, autonomous)
            SELECT v_command_id, job_ignore_errors, TRUE FROM cte_cmd
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
