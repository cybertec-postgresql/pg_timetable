DO $$
DECLARE
	v_task_id bigint;
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN
	-- An example for using the PROGRAM task.

	-- Create the base task
	INSERT INTO timetable.base_task(name, kind, script)
	VALUES ('run psql', 'PROGRAM'::timetable.task_kind, 'psql')
	RETURNING task_id INTO v_task_id;

	-- Create the chain
	INSERT INTO timetable.task_chain(task_id)
	VALUES (v_task_id)
	RETURNING chain_id INTO v_chain_id;

	-- Create the chain execution configuration
	INSERT INTO timetable.chain_execution_config (chain_id, chain_name, live)
	VALUES (v_chain_id, 'psql chain', TRUE)
	RETURNING chain_execution_config INTO v_chain_config_id;

	-- Create the parameters for the chain configuration
	INSERT INTO timetable.chain_execution_parameters (
		chain_execution_config,
		chain_id,
		order_id,
		value
	) VALUES (
		v_chain_config_id, v_chain_id, 1, ('[
			"-h", "' || host(inet_server_addr()) || '",
			"-p", "' || inet_server_port() || '",
			"-d", "' || current_database() || '",
			"-U", "' || current_user || '",
			"-c", "SELECT now();"
		]')::jsonb
	);
END $$
LANGUAGE 'plpgsql';