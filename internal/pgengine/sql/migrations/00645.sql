CREATE OR REPLACE FUNCTION timetable.is_cron_in_time(
    run_at timetable.cron, 
    ts timestamp
) RETURNS BOOLEAN AS $$
    SELECT
    CASE WHEN run_at IS NULL THEN
        TRUE
    ELSE
        date_part('month', ts) = ANY(a.months)
        AND (date_part('dow', ts) = ANY(a.dow) OR date_part('isodow', ts) = ANY(a.dow))
        AND date_part('day', ts) = ANY(a.days)
        AND date_part('hour', ts) = ANY(a.hours)
        AND date_part('minute', ts) = ANY(a.mins)
    END
    FROM
        timetable.cron_split_to_arrays(run_at) a
$$ LANGUAGE SQL;

DROP FUNCTION timetable.is_cron_in_time(timetable.cron, timestamp with time zone);

ALTER TABLE timetable.chain
    ADD COLUMN run_at_time_zone TEXT DEFAULT current_setting('TIMEZONE') NOT NULL;

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
    job_exclusive       BOOLEAN DEFAULT FALSE,
    job_on_error        TEXT DEFAULT NULL,
    job_time_zone       TEXT DEFAULT current_setting('TIMEZONE')
) RETURNS BIGINT AS $$
    WITH 
        cte_chain (v_chain_id) AS (
            INSERT INTO timetable.chain (chain_name, run_at, max_instances, live, self_destruct, client_name, exclusive_execution, on_error, run_at_time_zone) 
            VALUES (job_name, job_schedule,job_max_instances, job_live, job_self_destruct, job_client_name, job_exclusive, job_on_error, job_time_zone)
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

DROP FUNCTION timetable.add_job(TEXT, timetable.cron, TEXT, JSONB, timetable.command_kind, TEXT, INTEGER, BOOLEAN, BOOLEAN, BOOLEAN, BOOLEAN, TEXT);