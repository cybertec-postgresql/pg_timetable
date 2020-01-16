-- An example for Download task.
DO $$
DECLARE
	v_head_id bigint;
	v_chain_id bigint;
	v_chain_config_id bigint;
	v_task_id bigint;
BEGIN
	-- Step 1. Download file from the server
	-- Create the chain
	INSERT INTO timetable.task_chain (task_id, ignore_error)
	    VALUES (timetable.get_task_id ('Download'), TRUE)
	RETURNING
	    chain_id INTO v_head_id;

	-- Create the chain execution configuration with default values executed every minute
	INSERT INTO timetable.chain_execution_config 
		(chain_id, chain_name, live)
	VALUES 
		(v_head_id, 'Download locations and aggregate', TRUE)
	RETURNING
	    chain_execution_config INTO v_chain_config_id;

	-- Create the parameters for the step 1
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
		VALUES (v_chain_config_id, v_head_id, 1, '
				{
					"workersnum": 1, 
					"fileurls": ["https://www.cybertec-postgresql.com/secret/orte.txt"], 
					"destpath": "."
				}'::jsonb);
	
	RAISE NOTICE 'Step 1 completed. DownloadFile task added';

	-- Step 2. Transform Unicode characters into ASCII
	-- Create the shell task to call 'uconv -x' and name it 'unaccent'
	INSERT INTO timetable.base_task(name, kind, script)
		VALUES ('unaccent', 'SHELL'::timetable.task_kind, 'uconv')
	RETURNING 
		task_id INTO v_task_id;

	-- Add shell task 'unaccent' to the chain
	INSERT INTO timetable.task_chain (parent_id, task_id, ignore_error)
		VALUES (v_head_id, v_task_id, TRUE)
	RETURNING
	    chain_id INTO v_chain_id;

	-- Create the parameters for the 'unaccent' base task. Input and output files in this case
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
	    VALUES (v_chain_config_id, v_chain_id, 2, 
	    	'["-x", "Latin-ASCII", "-o", "orte_ansi.txt", "orte.txt"]'::jsonb);

	RAISE NOTICE 'Step 2 completed. Unacent task added';

	-- Step 3. Import ASCII file to PostgreSQL table using "psql \copy"
	-- Create the shell task to cal 'psql' and name it 'psql'
	INSERT INTO timetable.base_task(name, kind, script)
		VALUES ('psql', 'SHELL'::timetable.task_kind, 'psql')
	RETURNING 
		task_id INTO v_task_id;

	-- Add shell task 'psql' to the chain
	INSERT INTO timetable.task_chain (parent_id, task_id)
		VALUES (v_chain_id, v_task_id)
	RETURNING
	    chain_id INTO v_chain_id;

	-- Prepare the destination table 'location'
	CREATE TABLE IF NOT EXISTS location(name text);

	-- Add the parameters for the 'psql' base task. Execute client side \copy to 'location' from 'orte_ansi.txt'
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
	    VALUES (v_chain_config_id, v_chain_id, 3, ('[
			"-h", "' || host(inet_server_addr()) || '",
			"-p", "' || inet_server_port() || '",
			"-d", "' || current_database() || '",
			"-U", "' || current_user || '",
			"-c", "TRUNCATE location", 
			"-c", "\\copy location FROM orte_ansi.txt"
		]')::jsonb);

	RAISE NOTICE 'Step 3 completed. Import task added';
END;
$$
LANGUAGE 'plpgsql';
