CREATE OR REPLACE FUNCTION sleepy_func(text)
  RETURNS void LANGUAGE plpgsql AS
$BODY$ 
BEGIN 
   RAISE NOTICE 'Sleeping for 5 sec in %', $1;
   PERFORM pg_sleep_for('5 seconds');
   RAISE NOTICE 'Waiking up in %', $1;
END; 
$BODY$;

WITH 
chain_insert(task_id) AS (
    INSERT INTO timetable.task (command, ignore_error)
    VALUES ('SELECT sleepy_func($1)', TRUE) 
    RETURNING task_id
),
chain_config(id, chain_name) as (
    INSERT INTO timetable.chain (
        chain_id, 
        task_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live,
        self_destruct, 
        exclusive_execution 
    )  VALUES (
        DEFAULT, -- chain_id, 
        (SELECT task_id FROM chain_insert), -- task_id, 
        'exclusive sleepy task every 10 sec', -- chain_name, 
        '@after 10 seconds', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE -- exclusive_execution, 
    ), (
        DEFAULT, -- chain_id, 
        (SELECT task_id FROM chain_insert), -- task_id, 
        'exclusive sleepy task every 10 sec after previous', -- chain_name, 
        '@every 10 seconds', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        TRUE -- exclusive_execution, 
    ) 
    RETURNING  chain_id, chain_name
)
INSERT INTO timetable.parameter 
    (chain_id, task_id, order_id, value)
SELECT 
    chain_config.id,
    chain_insert.task_id,
    1,
    format('[ "Configuration %s" ]', chain_config.chain_name) :: jsonb
FROM chain_config, chain_insert;