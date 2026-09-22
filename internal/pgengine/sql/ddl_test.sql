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
