-- An advanced example showing how to use atutonomous tasks.
-- This one-task chain will execute test_proc() procedure.
-- Since procedure will make two commits (after f1() and f2())
-- we cannot use it as a regular task, because all regular tasks 
-- must be executed in the context of a single chain transaction.
-- Same rule applies for some other SQL commands, 
-- e.g. CREATE DATABASE, REINDEX, VACUUM, CREATE TABLESPACE, etc.
CREATE OR REPLACE FUNCTION f (msg TEXT) RETURNS void AS $$
BEGIN 
    RAISE notice '%', msg; 
END;
$$ LANGUAGE PLPGSQL;

CREATE OR REPLACE PROCEDURE test_proc () AS $$
BEGIN
    PERFORM f('hey 1');
    COMMIT;
    PERFORM f('hey 2');
    COMMIT;
END;
$$
LANGUAGE PLPGSQL;

WITH
    cte_chain (v_chain_id) AS (
        INSERT INTO timetable.chain (chain_name, run_at, max_instances, live, self_destruct) 
        VALUES (
            'call proc() every 10 sec', -- chain_name, 
            '@every 10 seconds',        -- run_at,
            1,     -- max_instances, 
            TRUE,  -- live, 
            FALSE -- self_destruct
        ) RETURNING chain_id
    ),
    cte_task(v_task_id) AS (
        INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error, autonomous)
        SELECT v_chain_id, 10, 'SQL', 'CALL test_proc()', TRUE, TRUE
        FROM cte_chain
        RETURNING task_id
    )
SELECT v_chain_id, v_task_id FROM cte_task, cte_chain;
