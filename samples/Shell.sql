-- An example for using the PROGRAM task.
DO $$
DECLARE
	v_command_id bigint;
	v_task_id bigint;
	v_chain_id bigint;
BEGIN

	-- Create the command
	INSERT INTO timetable.command(name, kind, script)
	VALUES ('run psql', 'PROGRAM'::timetable.command_kind, 'psql')
	RETURNING command_id INTO v_command_id;

	-- Create the chain
	INSERT INTO timetable.task(command_id)
	VALUES (v_command_id)
	RETURNING task_id INTO v_task_id;

	-- Create the chain execution configuration
	INSERT INTO timetable.chain (task_id, chain_name, live)
	VALUES (v_task_id, 'psql chain', TRUE)
	RETURNING chain_id INTO v_chain_id;

	-- Create the parameters for the chain configuration
	INSERT INTO timetable.parameter (
		chain_id,
		task_id,
		order_id,
		value
	) VALUES (
		v_chain_id, v_task_id, 1, ('[
			"-h", "' || host(inet_server_addr()) || '",
			"-p", "' || inet_server_port() || '",
			"-d", "' || current_database() || '",
			"-U", "' || current_user || '",
			"-c", "SELECT now();",
			"-w"
		]')::jsonb
	);
END $$
LANGUAGE 'plpgsql';