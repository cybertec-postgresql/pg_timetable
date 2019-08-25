DO $$
	-- An example for using the Log task.
DECLARE
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN
	-- Get the chain id
	v_chain_id := timetable.insert_base_task (task_name := 'Log',
        parent_task_id := NULL);
	
	-- Create the chain execution configuration
	INSERT INTO timetable.chain_execution_config
		VALUES (DEFAULT, -- chain_execution_config,
			v_chain_id, -- chain_id,
			'Builtin-in Log', -- chain_name
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
		)
		RETURNING
			chain_execution_config INTO v_chain_config_id;


	-- Chain Execution Parameters
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
        VALUES (v_chain_config_id, v_chain_id, 1, '{"Description":"Logs Execution"}'::jsonb);

END;
$$
LANGUAGE 'plpgsql';
