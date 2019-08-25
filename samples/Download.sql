DO $$
	-- An example of the Download task.
DECLARE
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN
	
	-- Get the chain id
	v_chain_id := timetable.insert_base_task (task_name := 'Download',
    parent_task_id := NULL);

	-- Create the chain execution configuration
	INSERT INTO timetable.chain_execution_config
		VALUES (DEFAULT, -- chain_execution_config,
			v_chain_id, -- chain_id,
			'Download a file', -- chain_name
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

	-- Create the parameters for the chain configuration

		-- "workersnum":   Number of workers - If the supplied value is less than one, a
		--                 worker will be created for each request. 
		-- "fileurls":     String array of URLs from which files will be downloaded
		-- "destpath":     Destination path 

	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
    VALUES (v_chain_config_id, v_chain_id, 1, '{
 				"workersnum":   1, 
 				"fileurls":   ["http://www.golang-book.com/public/pdf/gobook.pdf"], 
 				"destpath": "/Users/Lenovo/Downloads"
 			}'::jsonb);
END;
$$
LANGUAGE 'plpgsql';
