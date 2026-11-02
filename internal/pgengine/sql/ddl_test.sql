-- Tests for the functions in ddl.sql, run by pgcov (see .github/workflows/build.yml).

CREATE FUNCTION pg_temp.raises(stmt text, expected_state text) RETURNS text AS $$
BEGIN
    EXECUTE stmt;
    RAISE EXCEPTION 'expected SQLSTATE % from: %', expected_state, stmt;
EXCEPTION WHEN OTHERS THEN
    IF SQLSTATE <> expected_state THEN
        RAISE EXCEPTION 'expected SQLSTATE % but got %: %', expected_state, SQLSTATE, SQLERRM;
    END IF;
    RETURN SQLERRM;
END
$$ LANGUAGE plpgsql;

-- try_lock_client_name, get_client_name. Only sessions with this
-- application_name count as live, everything else is cleaned up.
SET application_name TO 'pg_timetable';

DO $$
BEGIN
    ASSERT timetable.get_client_name(pg_backend_pid()) IS NULL;

    -- leftovers from sessions that are gone
    INSERT INTO timetable.active_session VALUES (99, -1, 'gone');
    INSERT INTO timetable.active_chain VALUES (1, 'gone');

    ASSERT timetable.try_lock_client_name(1, 'worker');
    ASSERT timetable.get_client_name(pg_backend_pid()) = 'worker';
    ASSERT NOT EXISTS (SELECT 1 FROM timetable.active_session WHERE client_name = 'gone'), 'stale session removed';
    ASSERT NOT EXISTS (SELECT 1 FROM timetable.active_chain WHERE client_name = 'gone'), 'stale chain removed';

    ASSERT NOT timetable.try_lock_client_name(2, 'worker'), 'name taken by another client pid';
    ASSERT timetable.try_lock_client_name(1, 'worker'), 'same client pid may lock again';
    ASSERT timetable.try_lock_client_name(2, 'other');
    ASSERT timetable.try_lock_client_name(NULL, 'worker') IS NULL, 'STRICT';
END;
$$;

-- secret store without pgcrypto
DO $$
BEGIN
    ASSERT timetable.secret_count() = 0;
    ASSERT timetable.resolve_secret('missing', 'client', 'key') IS NULL, 'unknown secret needs no pgcrypto';

    PERFORM pg_temp.raises(
        $q$ INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ('client', 'bad name', '\x00') $q$,
        '23514');

    INSERT INTO timetable.secret (client_name, secret_name, value_enc, updated_at)
        VALUES ('client', 'token', '\x00', '2000-01-01');
    ASSERT timetable.secret_count() = 1;
    ASSERT pg_temp.raises($q$ SELECT timetable.resolve_secret('token', 'client', 'key') $q$, '0A000')
        LIKE 'pgcrypto extension is not installed%';

    -- secret_touch trigger
    ASSERT (SELECT updated_at FROM timetable.secret WHERE secret_name = 'token') = '2000-01-01';
    UPDATE timetable.secret SET value_enc = '\x01' WHERE secret_name = 'token';
    ASSERT (SELECT updated_at = now() AND updated_by = session_user FROM timetable.secret WHERE secret_name = 'token');
END;
$$;

-- secret store with pgcrypto
CREATE EXTENSION pgcrypto;

DO $$
BEGIN
    UPDATE timetable.secret SET value_enc = pgp_sym_encrypt('s3cret', 'key') WHERE secret_name = 'token';
    ASSERT timetable.resolve_secret('token', 'client', 'key') = 's3cret';
    ASSERT timetable.resolve_secret('token', 'other-client', 'key') IS NULL, 'scoped to client_name';
    PERFORM pg_temp.raises($q$ SELECT timetable.resolve_secret('token', 'client', 'wrong') $q$, '39000');
END;
$$;

-- check_parameter trigger; valid values are the examples from
-- docs/reference-commands-tasks-chains.md
DO $$
DECLARE
    v_chain BIGINT;
    v_task  BIGINT;
    c       RECORD;
BEGIN
    INSERT INTO timetable.chain (chain_name) VALUES ('params') RETURNING chain_id INTO v_chain;
    FOR c IN SELECT * FROM (VALUES
        ('SQL',     'SELECT $1', '[ "one", 2, 3.14, false ]', '"one"'),
        ('PROGRAM', 'iconv',     '["-x", "Latin-ASCII", "-o", "orte_ansi.txt", "orte.txt"]', '["-n", 5]'),
        ('BUILTIN', 'Sleep',     '5', '5.0'),
        ('BUILTIN', 'SendMail',  '{"username": "user@example.com", "serverport": 587}', '["user@example.com"]'),
        ('BUILTIN', 'Download',  '{"workersnum": 2, "fileurls": ["http://example.com/foo.gz"], "destpath": "."}', '"."'),
        ('BUILTIN', 'CopyFromFile',    '{"sql": "COPY location FROM STDIN", "filename": "orte_ansi.txt"}', '"COPY"'),
        ('BUILTIN', 'CopyToFile',      '{"sql": "COPY location TO STDOUT", "filename": "location.txt"}', '[]'),
        ('BUILTIN', 'CopyToProgram',   '{"sql": "COPY location TO STDOUT", "cmd": "sh", "args": ["-c"]}', '"sh"'),
        ('BUILTIN', 'CopyFromProgram', '{"sql": "COPY location FROM STDIN", "cmd": "gunzip", "args": ["-c"]}', '1')
    ) AS v(kind, command, good, bad) LOOP
        INSERT INTO timetable.task (chain_id, task_order, kind, command)
            VALUES (v_chain, 1, c.kind::timetable.command_kind, c.command) RETURNING task_id INTO v_task;
        INSERT INTO timetable.parameter VALUES (v_task, 1, c.good::jsonb);
        INSERT INTO timetable.parameter VALUES (v_task, 2, NULL), (v_task, 3, 'null');
        ASSERT pg_temp.raises(format('INSERT INTO timetable.parameter VALUES (%s, 4, %L)', v_task, c.bad), '23514')
            LIKE 'parameter for % task ' || v_task || ' must be %', c.command;
        PERFORM pg_temp.raises(format('UPDATE timetable.parameter SET value = %L WHERE task_id = %s', c.bad, v_task), '23514');
    END LOOP;

    -- Log, Shutdown, NoOp and unknown commands accept any value
    INSERT INTO timetable.task (chain_id, task_order, kind, command)
        VALUES (v_chain, 1, 'BUILTIN', 'Log') RETURNING task_id INTO v_task;
    INSERT INTO timetable.parameter VALUES (v_task, 1, '"WARNING"'), (v_task, 2, '{"Status": "WARNING"}');

    -- changing kind or command re-checks the stored values
    ASSERT pg_temp.raises(format('UPDATE timetable.task SET kind = %L WHERE task_id = %s', 'SQL', v_task), '23514')
        = format('parameter for SQL task %s must be a JSON array', v_task);
    PERFORM pg_temp.raises(format('UPDATE timetable.task SET command = %L WHERE task_id = %s', 'Sleep', v_task), '23514');
    UPDATE timetable.task SET command = 'NoOp' WHERE task_id = v_task;
END;
$$;
