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
	
	RAISE NOTICE 'Step 1 completed. DownloadFile task added with ID: %', v_chain_config_id;

	-- Step 2. Transform Unicode characters into ASCII
	-- Create the program task to call 'uconv -x' and name it 'unaccent'
	INSERT INTO timetable.base_task(name, kind, script)
		VALUES ('unaccent', 'PROGRAM'::timetable.task_kind, 'uconv')
	RETURNING 
		task_id INTO v_task_id;

	-- Add program task 'unaccent' to the chain
	INSERT INTO timetable.task_chain (parent_id, task_id, ignore_error)
		VALUES (v_head_id, v_task_id, TRUE)
	RETURNING
	    chain_id INTO v_chain_id;

	-- Create the parameters for the 'unaccent' base task. Input and output files in this case
	-- Under Windows we should call PowerShell instead of "uconv" with command:
	-- Set-content "orte_ansi.txt" ((Get-content "orte.txt").Normalize("FormD") -replace '\p{M}', '')
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
	    VALUES (v_chain_config_id, v_chain_id, 1, '["-x", "Latin-ASCII", "-o", "orte_ansi.txt", "orte.txt"]'::jsonb);

	RAISE NOTICE 'Step 2 completed. Unacent task added';

	-- Step 3. Import ASCII file to PostgreSQL table using "psql \copy"
	-- Add PROGRAM task 'psql' to the chain
	INSERT INTO timetable.task_chain (parent_id, task_id)
		VALUES (v_chain_id, timetable.get_task_id ('CopyFromFile'))
	RETURNING
	    chain_id INTO v_chain_id;

	-- Prepare the destination table 'location'
	CREATE TABLE IF NOT EXISTS location(name text);

	-- Add the parameters for the 'psql' base task. Execute client side \copy to 'location' from 'orte_ansi.txt'
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
	    VALUES (v_chain_config_id, v_chain_id, 1, '{"sql": "COPY location FROM STDIN", "filename": "orte_ansi.txt" }'::jsonb);

	RAISE NOTICE 'Step 3 completed. Import task added';
END;
$$
LANGUAGE 'plpgsql';
