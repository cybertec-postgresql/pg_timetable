DO $$
    -- An example for using the SendMail task.
DECLARE
    v_mail_task_id bigint;
    v_log_task_id bigint;
    v_chain_id bigint;
BEGIN
    -- Get the chain id
    INSERT INTO timetable.chain (chain_name, max_instances, live) VALUES ('Send Mail', 1, TRUE)
    RETURNING chain_id INTO v_chain_id;

    -- Add SendMail task
    INSERT INTO timetable.task (chain_id, task_order, kind, command) 
    SELECT v_chain_id, 10, 'BUILTIN', 'SendMail'
    RETURNING task_id INTO v_mail_task_id;

    -- Create the parameters for the SensMail task
        -- "username":	      The username used for authenticating on the mail server
        -- "password":        The password used for authenticating on the mail server
        -- "serverhost":      The IP address or hostname of the mail server
        -- "serverport":      The port of the mail server
        -- "senderaddr":      The email that will appear as the sender
        -- "ccaddr":	      String array of the recipients(Cc) email addresses
        -- "bccaddr":	      String array of the recipients(Bcc) email addresses
        -- "toaddr":          String array of the recipients(To) email addresses
        -- "subject":	      Subject of the email
        -- "attachment":      String array of the attachments (local file)
        -- "attachmentdata":  Pairs of name and base64-encoded content
        -- "msgbody":	      The body of the email

    INSERT INTO timetable.parameter (task_id, order_id, value)
        VALUES (v_mail_task_id, 1, '{
                "username":     "user@example.com",
                "password":     "password",
                "serverhost":   "smtp.example.com",
                "serverport":   587,
                "senderaddr":   "user@example.com",
                "ccaddr":       ["recipient_cc@example.com"],
                "bccaddr":      ["recipient_bcc@example.com"],
                "toaddr":       ["recipient@example.com"],
                "subject":      "pg_timetable - No Reply",
                "attachment":   ["D:\\Go stuff\\Books\\Concurrency in Go.pdf","report.yaml"],
                "attachmentdata": [{"name": "File.txt", "base64data": "RmlsZSBDb250ZW50"}],
                "msgbody":      "<b>Hello User,</b> <p>I got some Go books for you enjoy</p> <i>pg_timetable</i>!",
                "contenttype":  "text/html; charset=UTF-8"
                }'::jsonb);
    
    -- Add Log task and make it the last task using `task_order` column (=30)
    INSERT INTO timetable.task (chain_id, task_order, kind, command) 
    SELECT v_chain_id, 30, 'BUILTIN', 'Log'
    RETURNING task_id INTO v_log_task_id;

    -- Add housekeeping task, that will delete sent mail and update parameter for the previous logging task
    -- Since we're using special add_task() function we don't need to specify the `chain_id`.
    -- Function will take the same `chain_id` from the parent task, SendMail in this particular case
    PERFORM timetable.add_task(
        kind => 'SQL', 
        parent_id => v_mail_task_id,
        command => format(
$query$WITH sent_mail(toaddr) AS (DELETE FROM timetable.parameter WHERE task_id = %s RETURNING value->>'username')
INSERT INTO timetable.parameter (task_id, order_id, value) 
SELECT %s, 1, to_jsonb('Sent emails to: ' || string_agg(sent_mail.toaddr, ';'))
FROM sent_mail
ON CONFLICT (task_id, order_id) DO UPDATE SET value = EXCLUDED.value$query$, 
                v_mail_task_id, v_log_task_id
            ),
        order_delta => 10
    );

-- In the end we should have something like this. Note, that even Log task was created earlier it will be executed later
-- due to `task_order` column.

-- timetable=> SELECT task_id, chain_id, kind, left(command, 50) FROM timetable.task ORDER BY task_order;  
--  task_id | chain_id | task_order |  kind   |                             left
-- ---------+----------+------------+---------+---------------------------------------------------------------
--       45 |       24 |         10 | BUILTIN | SendMail
--       47 |       24 |         20 | SQL     | WITH sent_mail(toaddr) AS (DELETE FROM timetable.p
--       46 |       24 |         30 | BUILTIN | Log
-- (3 rows)

END;
$$
LANGUAGE PLPGSQL;
