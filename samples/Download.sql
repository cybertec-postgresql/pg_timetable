DO $$
	-- An example of the Download task.
DECLARE
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN

	-- Get the chain id
	v_chain_id := timetable.insert_base_task('Download', NULL);

	-- Create the chain execution configuration
	INSERT INTO timetable.chain_execution_config (chain_id, chain_name, max_instances, live)
		VALUES (v_chain_id, 'Download a file', 1, TRUE)
		RETURNING
			chain_execution_config INTO v_chain_config_id;

	-- Create the parameters for the chain configuration
		-- "workersnum":   Number of workers - If the supplied value is less than one, a
		--                 worker will be created for each request.
		-- "fileurls":     String array of URLs from which files will be downloaded
		-- "destpath":     Destination path

	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
    VALUES (v_chain_config_id, v_chain_id, 1, '{
 				"workersnum":	1,
 				"fileurls":		["http://www.golang-book.com/public/pdf/gobook.pdf"],
 				"destpath":		"/Users/Lenovo/Downloads"
 			}'::jsonb);

END;
$$
LANGUAGE 'plpgsql';
