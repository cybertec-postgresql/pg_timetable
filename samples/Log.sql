DO $$
	-- An example for using the Log task.
DECLARE
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN

	-- Get the chain id
	v_chain_id := timetable.insert_base_task('Log', NULL);

	-- Create the chain execution configuration
	INSERT INTO timetable.chain_execution_config (chain_id, chain_name, max_instances, live)
		VALUES (v_chain_id, 'Builtin-in Log', 1, TRUE)
		RETURNING
			chain_execution_config INTO v_chain_config_id;

	-- Chain Execution Parameters
	INSERT INTO timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value)
        VALUES (v_chain_config_id, v_chain_id, 1, '{"Description": "Logs Execution"}'::jsonb);

END;
$$
LANGUAGE 'plpgsql';
