DO $$
	-- An example for using the SendMail task.
DECLARE
	v_task_id bigint;
	v_chain_id bigint;
	v_chain_config_id bigint;
BEGIN
	-- Get the base task id
	SELECT task_id INTO v_task_id FROM timetable.base_task WHERE name = 'SendMail';
	
	-- Create the chain
	INSERT INTO timetable.task_chain(task_id)
	VALUES (v_task_id)
	RETURNING chain_id INTO v_chain_id;

	-- Create the chain execution configuration
	INSERT INTO timetable.chain_execution_config (chain_id, chain_name, live)
	VALUES (v_chain_id, 'Send Mail', TRUE)
	RETURNING chain_execution_config INTO v_chain_config_id;

	INSERT INTO timetable.chain_execution_config VALUES 
    	(
        DEFAULT, -- chain_execution_config, 
        v_chain_id, -- chain_id, 
        'Send Mail', -- chain_name
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
    RETURNING  chain_execution_config INTO v_chain_config_id;

	-- Create the parameters for the chain configuration
		-- "username":   The username used for authenticating on the mail server
		-- "password":    Sender Mail id Password
		-- "serverhost":  The IP address or hostname of the mail server
		-- "serverport":  The port of the mail server
		-- "senderaddr":  The email that will appear as the sender
		-- "toaddr":      Reciever mail Id, You can add multiple comma separated reciver ids, 
		-- "msgbody":	  Email Body
	INSERT INTO timetable.chain_execution_parameters (
		chain_execution_config,
		chain_id,
		order_id,
		value
		) 
		VALUES (
		v_chain_config_id, v_chain_id, 1, '{
			"username":   "Userid@example.com", 
			"password":   "Password", 
			"serverhost": "smtp.example.com",
			"serverport":"587",
			"senderaddr": "Userid@example.com",
			"toaddr":     ["toAddr@example.com"],
			"msgbody":	  "Hello, Its pg_timetable test"
			}'::jsonb
		);

END;
$$
LANGUAGE 'plpgsql';
