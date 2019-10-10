INSERT INTO timetable.chain_execution_config VALUES 
(
    DEFAULT, -- chain_execution_config, 
    timetable.insert_base_task(task_name := 'NoOp', parent_task_id := NULL), -- chain_id, 
    'execute noop every minute', -- chain_name, 
    NULL, -- run_at_minute, 
    NULL, -- run_at_hour, 
    NULL, -- run_at_day, 
    NULL, -- run_at_month,
    NULL, -- run_at_day_of_week, 
    1, -- max_instances, 
    TRUE, -- live, 
    FALSE, -- self_destruct,
	FALSE, -- exclusive_execution, 
    NULL -- excluded_execution_configs
);