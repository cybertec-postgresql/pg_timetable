DO $$
	-- An example for using the Log task.
DECLARE
	v_command_id bigint;
	v_task_id bigint;
	v_chain_config_id bigint;
BEGIN
	-- Get the base task id
	SELECT command_id INTO v_command_id FROM timetable.command WHERE name = 'Log';
	
	-- Create the chain
	INSERT INTO timetable.task(command_id)
	VALUES (v_command_id)
	RETURNING task_id INTO v_task_id;

	-- Create the chain execution configuration
	INSERT INTO timetable.chain  (
        chain_id, 
        task_id, 
        chain_name, 
        run_at, 
        max_instances, 
        live,
        self_destruct, 
        exclusive_execution 
    ) VALUES (
        DEFAULT, -- chain_id, 
        v_task_id, -- task_id, 
        'Builtin-in Log', -- chain_name
        '* * * * *', -- run_at, 
        1, -- max_instances, 
        TRUE, -- live, 
        FALSE, -- self_destruct,
        FALSE -- exclusive_execution, 
    	)
    RETURNING  chain_id INTO v_chain_config_id;


	-- Chain Execution Parameters
	INSERT INTO timetable.parameter (
		chain_id,
		task_id,
		order_id,
		value
	) VALUES (
		v_chain_config_id,
		v_task_id, 
		1, 
        '{"Description":"Logs Execution"}'::jsonb
	);

END;
$$
LANGUAGE 'plpgsql';
