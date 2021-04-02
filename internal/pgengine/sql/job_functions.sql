-- get_running_jobs() returns jobs are running for particular chain_execution_config
CREATE OR REPLACE FUNCTION timetable.get_running_jobs(BIGINT) RETURNS SETOF record AS $$
    SELECT  chain_execution_config, start_status
        FROM    timetable.run_status
        WHERE   start_status IN ( SELECT   start_status
                FROM    timetable.run_status
                WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED',
                             'CHAIN_DONE', 'DEAD')
                    AND (chain_execution_config = $1 OR chain_execution_config = 0)
                GROUP BY 1
                HAVING count(*) < 2 
                ORDER BY 1)
            AND chain_execution_config = $1 
        GROUP BY 1, 2
        ORDER BY 1, 2 DESC
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.insert_base_task(IN task_name TEXT, IN parent_task_id BIGINT) RETURNS BIGINT AS $$
DECLARE
    builtin_id BIGINT;
    result_id BIGINT;
BEGIN
    SELECT task_id FROM timetable.base_task WHERE name = task_name INTO builtin_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Nonexistent builtin task --> %', task_name
        USING 
            ERRCODE = 'invalid_parameter_value',
            HINT = 'Please check your user task name parameter';
    END IF;
    INSERT INTO timetable.task_chain 
        (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
    VALUES 
        (DEFAULT, parent_task_id, builtin_id, NULL, NULL, FALSE)
    RETURNING chain_id INTO result_id;
    RETURN result_id;
END
$$ LANGUAGE PLPGSQL;

-- job_add() will add job to the system
CREATE OR REPLACE FUNCTION timetable.job_add(
    task_name        TEXT,
    task_function    TEXT,
    client_name      TEXT,
    task_type        timetable.task_kind DEFAULT 'SQL'::timetable.task_kind,
    run_at           timetable.cron DEFAULT NULL,
    max_instances    INTEGER DEFAULT NULL,
    live             BOOLEAN DEFAULT false,
    self_destruct    BOOLEAN DEFAULT false
) RETURNS BIGINT AS $$
    WITH 
        cte_task(v_task_id) AS ( --Create task
            INSERT INTO timetable.base_task 
            VALUES (DEFAULT, task_name, task_type, task_function)
            RETURNING task_id
        ),
        cte_chain(v_chain_id) AS ( --Create chain
            INSERT INTO timetable.task_chain (task_id, ignore_error)
            SELECT v_task_id, TRUE FROM cte_task
            RETURNING chain_id
        )
    INSERT INTO timetable.chain_execution_config (
        chain_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live,
        self_destruct,
        client_name
    ) SELECT 
        v_chain_id, 
        'chain_' || v_chain_id, 
        run_at,
        max_instances, 
        live, 
        self_destruct,
        client_name
    FROM cte_chain
    RETURNING chain_execution_config 
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_start(chain_config_id BIGINT, worker_name TEXT)
RETURNS void AS $$
  SELECT pg_notify(
  	worker_name, 
	format('{"ConfigID": %s, "Command": "START", "Ts": %s}', 
		chain_config_id, 
		EXTRACT(epoch FROM clock_timestamp())::bigint)
	)
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_stop(chain_config_id BIGINT, worker_name TEXT)
RETURNS void AS  $$ 
  SELECT pg_notify(
  	worker_name, 
	format('{"ConfigID": %s, "Command": "STOP", "Ts": %s}', 
		chain_config_id, 
		EXTRACT(epoch FROM clock_timestamp())::bigint)
	)
$$ LANGUAGE SQL;
