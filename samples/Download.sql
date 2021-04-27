-- An enhanced example consisting of three tasks:
-- 1. Download text file from internet using BUILT-IN command
-- 2. Remove accents (diacritic signs) from letters using PROGRAM command (can be done with `unaccent` PostgreSQL extension) 
-- 3. Import text file as CSV file using BUILT-IN command (can be down with `psql -c /copy`)
DO $$
DECLARE
	v_head_id bigint;
	v_task_id bigint;
	v_chain_id bigint;
	v_command_id bigint;
BEGIN
	-- Step 1. Download file from the server
	-- Create the chain
	INSERT INTO timetable.task (command_id, ignore_error)
	    VALUES (timetable.get_command_id ('Download'), TRUE)
	RETURNING
	    task_id INTO v_head_id;

	-- Create the chain with default values executed every minute (NULL == '* * * * *' :: timetable.cron)
	INSERT INTO timetable.chain 
		(task_id, chain_name, live)
	VALUES 
		(v_head_id, 'Download locations and aggregate', TRUE)
	RETURNING
	    chain_id INTO v_chain_id;

	-- Create the parameters for the step 1:
	INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
		VALUES (v_chain_id, v_head_id, 1, '
				{
					"workersnum": 1,
					"fileurls": ["https://www.cybertec-postgresql.com/secret/orte.txt"], 
					"destpath": "."
				}'::jsonb);
	
	RAISE NOTICE 'Step 1 completed. DownloadFile task added with ID: %', v_chain_id;

	-- Step 2. Transform Unicode characters into ASCII
	-- Create the program task to call 'uconv' and name it 'unaccent'
	INSERT INTO timetable.command(name, kind, script)
		VALUES ('unaccent', 'PROGRAM'::timetable.command_kind, 'uconv')
	RETURNING 
		command_id INTO v_command_id;

	-- Add program task 'unaccent' to the chain
	INSERT INTO timetable.task (parent_id, command_id, ignore_error)
		VALUES (v_head_id, v_command_id, TRUE)
	RETURNING
	    task_id INTO v_task_id;

	-- Create the parameters for the 'unaccent' task. Input and output files in this case
	-- Under Windows we should call PowerShell instead of "uconv" with command:
	-- Set-content "orte_ansi.txt" ((Get-content "orte.txt").Normalize("FormD") -replace '\p{M}', '')
	INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
	    VALUES (v_chain_id, v_task_id, 1, '["-x", "Latin-ASCII", "-o", "orte_ansi.txt", "orte.txt"]'::jsonb);

	RAISE NOTICE 'Step 2 completed. Unacent task added';

	-- Step 3. Import ASCII file to PostgreSQL table using "CopyFromFile" built-in command
	INSERT INTO timetable.task (parent_id, command_id)
		VALUES (v_task_id, timetable.get_command_id ('CopyFromFile'))
	RETURNING
	    task_id INTO v_task_id;

	-- Prepare the destination table 'location'
	CREATE TABLE IF NOT EXISTS location(name text);

	-- Add the parameters for the download task. Execute client side COPY to 'location' from 'orte_ansi.txt'
	INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
	    VALUES (v_chain_id, v_task_id, 1, '{"sql": "COPY location FROM STDIN", "filename": "orte_ansi.txt" }'::jsonb);

	RAISE NOTICE 'Step 3 completed. Import task added';
END;
$$ LANGUAGE PLPGSQL;
