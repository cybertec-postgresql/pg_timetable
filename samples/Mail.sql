DO $$
	-- An example for using the SendMail task.
DECLARE
	v_task_id bigint;
	v_chain_config_id bigint;
BEGIN

	-- Get the chain id
	v_task_id := timetable.add_task('SendMail', NULL);

	INSERT INTO timetable.chain (task_id, chain_name, max_instances, live)
		VALUES (v_task_id, 'Send Mail', 1, TRUE)
	RETURNING
		chain_id INTO v_chain_config_id;

	-- Create the parameters for the chain configuration
		-- "username":	  The username used for authenticating on the mail server
		-- "password":    The password used for authenticating on the mail server
		-- "serverhost":  The IP address or hostname of the mail server
		-- "serverport":  The port of the mail server
		-- "senderaddr":  The email that will appear as the sender
		-- "ccaddr":	  String array of the recipients(Cc) email addresses
		-- "bccaddr":	  String array of the recipients(Bcc) email addresses
		-- "toaddr":      String array of the recipients(To) email addresses
		-- "subject":	  Subject of the email
		-- "attachment":  String array of the attachments
		-- "msgbody":	  The body of the email

	INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
		VALUES (v_chain_config_id, v_task_id, 1, '{
				"username":     "user@example.com",
				"password":		"password",
				"serverhost":	"smtp.example.com",
				"serverport":	587,
				"senderaddr":   "user@example.com",
				"ccaddr":		["recipient_cc@example.com"],
				"bccaddr":		["recipient_bcc@example.com"],
				"toaddr":       ["recipient@example.com"],
				"subject": 		"pg_timetable - No Reply",
				"attachment":   ["D:\\Go stuff\\Books\\Concurrency in Go.pdf","D:\\Go stuff\\Books\\The Way To Go.pdf"],
				"msgbody":		"<b>Hello User,</b> <p>I got some Go books for you enjoy</p> <i>pg_timetable</i>!"
				}'::jsonb);

END;
$$
LANGUAGE 'plpgsql';
