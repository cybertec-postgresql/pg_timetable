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
sql_task(id) AS (
    INSERT INTO timetable.base_task VALUES (
        DEFAULT,                     -- task_id
        'execute sleepy fucntions',  -- name
        DEFAULT,                     -- 'SQL' :: timetable.task_kind
        'SELECT sleepy_func($1)'     -- task script
    )
    RETURNING task_id
),
chain_insert(chain_id) AS (
    INSERT INTO timetable.task_chain 
        (task_id, ignore_error)
    SELECT 
        id, TRUE
    FROM sql_task
    RETURNING chain_id
),
chain_config(id, chain_name) as (
    INSERT INTO timetable.chain_execution_config (
        chain_execution_config, 
        chain_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live,
        self_destruct, 
        exclusive_execution, 
        excluded_execution_configs
    )  VALUES (
        DEFAULT, -- chain_execution_config, 
        (SELECT chain_id FROM chain_insert), -- chain_id, 
        'run sleepy task every 10 sec', -- chain_name, 
        '@every 10 seconds', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE, -- exclusive_execution, 
        NULL -- excluded_execution_configs
    ), (
        DEFAULT, -- chain_execution_config, 
        (SELECT chain_id FROM chain_insert), -- chain_id, 
        'exclusive sleepy task every 10 sec', -- chain_name, 
        '@every 10 seconds', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        TRUE, -- exclusive_execution, 
        NULL -- excluded_execution_configs
    ) 
    RETURNING  chain_execution_config, chain_name
)
INSERT INTO timetable.chain_execution_parameters 
    (chain_execution_config, chain_id, order_id, value)
SELECT 
    chain_config.id,
    chain_insert.chain_id,
    1,
    format('[ "Configuration %s" ]', chain_config.chain_name) :: jsonb
FROM chain_config, chain_insert;