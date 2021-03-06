WITH 
sql_task(id) AS (
    INSERT INTO timetable.base_task VALUES (
		DEFAULT, 						-- task_id
		'notify channel with payload',	-- name
		DEFAULT, 						-- 'SQL' :: timetable.task_kind
		'SELECT pg_notify($1, $2)'		-- task script
	)
	RETURNING task_id
),
chain_insert(chain_id) AS (
    INSERT INTO timetable.task_chain 
        (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
    VALUES 
        (DEFAULT, NULL, (SELECT id FROM sql_task), NULL, NULL, TRUE)
    RETURNING chain_id
),
chain_config(id) as (
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
    ) VALUES (
        DEFAULT, -- chain_execution_config, 
        (SELECT chain_id FROM chain_insert), -- chain_id, 
        'notify every minute', -- chain_name, 
        '* * * * *', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE, -- exclusive_execution, 
        NULL -- excluded_execution_configs
    )
    RETURNING  chain_execution_config
)
INSERT INTO timetable.chain_execution_parameters 
    (chain_execution_config, chain_id, order_id, value)
VALUES (
    (SELECT id FROM chain_config),
    (SELECT chain_id FROM chain_insert),
    1,
    '[ "TT_CHANNEL", "Ahoj from SQL base task" ]' :: jsonb) 