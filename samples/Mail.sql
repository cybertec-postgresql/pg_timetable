-- Mail.sql demonstrates SendMail with a stored secret.
--
-- pg_timetable itself NEVER installs the pgcrypto extension. It is an
-- optional dependency of the secret store, provisioned by the database
-- administrator. As a demo a user runs deliberately, this sample installs
-- pgcrypto itself and uses the unqualified pgp_sym_encrypt call.
-- Production chains should remove the CREATE EXTENSION line and rely on
-- the DBA having installed pgcrypto.
--
-- Decision: client_name is derived from
-- `pg_timetable.current_client_name` via current_setting() so the sample is
-- self-contained under TestSamplesScripts; the test harness sets the matching
-- fixed encryption key in internal/testutils/testcontainers.go.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
    -- An example for using the SendMail task.
DECLARE
    v_mail_task_id bigint;
    v_log_task_id bigint;
    v_chain_id bigint;
    v_client_name text;
BEGIN
    -- Resolve the client_name from the session setting when available;
    -- fall back to a literal placeholder for ad-hoc execution.
    v_client_name := coalesce(
        nullif(current_setting('pg_timetable.current_client_name', true), ''),
        'sample_client');

    -- Store the SMTP password encrypted. pgcrypto is required for the secret
    -- store; here it lives in `public` (the default), so the call is
    -- unqualified.
    INSERT INTO timetable.secret (client_name, secret_name, value_enc)
    VALUES (v_client_name, 'smtp_main',
            pgp_sym_encrypt('s3cr3t pw''s', 'pgtt_test_secret_key'))
    ON CONFLICT (client_name, secret_name) DO UPDATE
        SET value_enc = EXCLUDED.value_enc;

    -- Get the chain id
    INSERT INTO timetable.chain (chain_name, max_instances, live) VALUES ('send_mail', 1, TRUE)
    RETURNING chain_id INTO v_chain_id;

    -- Add SendMail task
    INSERT INTO timetable.task (chain_id, task_order, kind, command)
    SELECT v_chain_id, 10, 'BUILTIN', 'SendMail'
    RETURNING task_id INTO v_mail_task_id;

    -- Create the parameters for the SensMail task
        -- "username":        The username used for authenticating on the mail server
        -- "password":        The password used for authenticating on the mail server
        -- "serverhost":      The IP address or hostname of the mail server
        -- "serverport":      The port of the mail server
        -- "senderaddr":      The email that will appear as the sender
        -- "ccaddr":      String array of the recipients(Cc) email addresses
        -- "bccaddr":     String array of the recipients(Bcc) email addresses
        -- "toaddr":          String array of the recipients(To) email addresses
        -- "subject":     Subject of the email
        -- "attachment":      String array of the attachments (local file)
        -- "attachmentdata":  Pairs of name and base64-encoded content
        -- "msgbody":     The body of the email

    INSERT INTO timetable.parameter (task_id, order_id, value)
        VALUES (v_mail_task_id, 1, '{
                "username":     "user@example.com",
                "password":     "${secret:smtp_main}",
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
    -- Legacy (deprecated): inline literal in parameter.value still works
    -- unchanged. ${secret:...} is opt-in syntax, not a format change.
    -- "password":     "literal-insecure-password",

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
-- (3 rows);

END;
$$
LANGUAGE PLPGSQL;
