DO $$
	-- An example for using the Log task.
DECLARE
	v_task_id bigint;
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN
	-- Get the base task id
	SELECT task_id INTO v_task_id FROM timetable.base_task WHERE name = 'Log';
	
	-- Create the chain
	INSERT INTO timetable.task_chain(task_id)
	VALUES (v_task_id)
	RETURNING chain_id INTO v_chain_id;

	-- Create the chain execution configuration
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
        v_chain_id, -- chain_id, 
        'Builtin-in Log', -- chain_name
        '* * * * *', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE, -- exclusive_execution, 
        NULL -- excluded_execution_configs
    	)
    RETURNING  chain_execution_config INTO v_chain_config_id;


	-- Chain Execution Parameters
	INSERT INTO timetable.chain_execution_parameters (
		chain_execution_config,
		chain_id,
		order_id,
		value
	) VALUES (
		v_chain_config_id,
		v_chain_id, 
		1, 
        '{"Description":"Logs Execution"}'::jsonb
	);

END;
$$
LANGUAGE 'plpgsql';
