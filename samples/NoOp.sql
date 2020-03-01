INSERT INTO timetable.chain_execution_config  (
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
    timetable.insert_base_task(task_name := 'NoOp', parent_task_id := NULL), -- chain_id, 
    'execute noop every minute', -- chain_name, 
    '* * * * *', -- run_at, 
    1, -- max_instances, 
    TRUE, -- live, 
    FALSE, -- self_destruct,
	FALSE, -- exclusive_execution, 
    NULL -- excluded_execution_configs
);