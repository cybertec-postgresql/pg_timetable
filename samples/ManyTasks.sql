WITH 
    cte_chain (v_chain_id) AS ( -- let's create a new chain and add tasks to it later
        INSERT INTO timetable.chain (chain_name, run_at, max_instances, live) 
        VALUES ('many tasks', '* * * * *', 1, true)
        RETURNING chain_id
    ),
    cte_tasks(v_task_id) AS ( -- now we'll add 500 tasks to the chain, some of them will fail
        INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error)
        SELECT v_chain_id, g.s, 'SQL', 'SELECT 1.0 / round(random())::int4;', true
        FROM cte_chain, generate_series(1, 500) AS g(s)
        RETURNING task_id
    ),
    report_task(v_task_id) AS ( -- and the last reporting task will calculate the statistic
        INSERT INTO timetable.task (chain_id, task_order, kind, command)
        SELECT v_chain_id, 501, 'SQL', $CMD$DO
$$
DECLARE
    s TEXT;
BEGIN
    WITH report AS (
        SELECT 
        count(*) FILTER (WHERE returncode = 0) AS success,
        count(*) FILTER (WHERE returncode != 0) AS fail,
        count(*) AS total
        FROM timetable.execution_log 
        WHERE chain_id = current_setting('pg_timetable.current_chain_id')::bigint
          AND txid = txid_current()
    )
    SELECT 'Tasks executed:' || total || 
         '; succeeded: ' || success || 
         '; failed: ' || fail || 
         '; ratio: ' || 100.0*success/GREATEST(total,1)
    INTO s
    FROM report;
    RAISE NOTICE '%', s;
END;
$$
$CMD$
        FROM cte_chain
        RETURNING task_id
    )
SELECT v_chain_id FROM cte_chain