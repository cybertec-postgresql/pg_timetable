DO $$
DECLARE
	v_task_id bigint;
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN
	-- An example for using the SHELL task.

	-- Create the base task
	INSERT INTO timetable.base_task(name, kind, script)
	VALUES ('psql', 'SHELL'::timetable.task_kind, 'psql')
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
		v_chain_config_id, v_chain_id, 1, '[
			"-h", "localhost",
			"-p", "5432",
			"-d", "template1",
			"-U", "postgres",
			"-c", "SELECT now();"
		]'::jsonb
	);
END $$
LANGUAGE 'plpgsql';