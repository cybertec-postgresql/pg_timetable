-- get_running_jobs() returns jobs are running for particular chain_id
CREATE OR REPLACE FUNCTION timetable.get_running_jobs(BIGINT) RETURNS SETOF record AS $$
    SELECT  chain_id, start_status
        FROM    timetable.run_status
        WHERE   start_status IN ( SELECT   start_status
                FROM    timetable.run_status
                WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED',
                             'CHAIN_DONE', 'DEAD')
                    AND (chain_id = $1 OR chain_id = 0)
                GROUP BY 1
                HAVING count(*) < 2 
                ORDER BY 1)
            AND chain_id = $1 
        GROUP BY 1, 2
        ORDER BY 1, 2 DESC
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.add_task(IN command_name TEXT, IN parent_task_id BIGINT) RETURNS BIGINT AS $$
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
    job_client_name     TEXT DEFAULT NULL,
    job_type            timetable.command_kind DEFAULT 'SQL'::timetable.command_kind,
    job_max_instances   INTEGER DEFAULT NULL,
    job_live            BOOLEAN DEFAULT TRUE,
    job_self_destruct   BOOLEAN DEFAULT FALSE,
    job_ignore_errors   BOOLEAN DEFAULT TRUE
) RETURNS BIGINT AS $$
    WITH 
        cte_task(v_command_id) AS ( --Create task
            INSERT INTO timetable.command (command_id, name, kind, script)
            VALUES (DEFAULT, job_name, job_type, job_command)
            RETURNING command_id
        ),
        cte_chain(v_task_id) AS ( --Create chain
            INSERT INTO timetable.task (command_id, ignore_error, autonomous)
            SELECT v_command_id, job_ignore_errors, TRUE FROM cte_task
            RETURNING task_id
        )
    INSERT INTO timetable.chain (
        task_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live,
        self_destruct,
        client_name
    ) SELECT 
        v_task_id, 
        job_name, 
        job_schedule,
        job_max_instances, 
        job_live, 
        job_self_destruct,
        job_client_name
    FROM cte_chain
    RETURNING chain_id 
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_start(chain_id BIGINT, worker_name TEXT)
RETURNS void AS $$
  SELECT pg_notify(
  	worker_name, 
	format('{"ConfigID": %s, "Command": "START", "Ts": %s}', 
		chain_id, 
		EXTRACT(epoch FROM clock_timestamp())::bigint)
	)
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_stop(chain_id BIGINT, worker_name TEXT)
RETURNS void AS  $$ 
  SELECT pg_notify(
  	worker_name, 
	format('{"ConfigID": %s, "Command": "STOP", "Ts": %s}', 
		chain_id, 
		EXTRACT(epoch FROM clock_timestamp())::bigint)
	)
$$ LANGUAGE SQL;
