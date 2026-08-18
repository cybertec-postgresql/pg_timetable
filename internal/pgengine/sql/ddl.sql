CREATE TABLE timetable.chain (
    chain_id            BIGSERIAL   PRIMARY KEY,
    chain_name          TEXT        NOT NULL UNIQUE,
    run_at              timetable.cron,
    max_instances       INTEGER,
    timeout             INTEGER     DEFAULT 0,
    live                BOOLEAN     DEFAULT FALSE,
    self_destruct       BOOLEAN     DEFAULT FALSE,
    exclusive_execution BOOLEAN     DEFAULT FALSE,
    client_name         TEXT,
    on_error            TEXT
);

COMMENT ON TABLE timetable.chain IS
    'Stores information about chains schedule';
COMMENT ON COLUMN timetable.chain.run_at IS
    'Extended CRON-style time notation the chain has to be run at';
COMMENT ON COLUMN timetable.chain.max_instances IS
    'Number of instances (clients) this chain can run in parallel';
COMMENT ON COLUMN timetable.chain.timeout IS
    'Abort any chain that takes more than the specified number of milliseconds';
COMMENT ON COLUMN timetable.chain.live IS
    'Indication that the chain is ready to run, set to FALSE to pause execution';
COMMENT ON COLUMN timetable.chain.self_destruct IS
    'Indication that this chain will delete itself after successful run';
COMMENT ON COLUMN timetable.chain.exclusive_execution IS
    'All parallel chains should be paused while executing this chain';
COMMENT ON COLUMN timetable.chain.client_name IS
    'Only client with this name is allowed to run this chain, set to NULL to allow any client';    

CREATE TYPE timetable.command_kind AS ENUM ('SQL', 'PROGRAM', 'BUILTIN');

CREATE TABLE timetable.task (
    task_id             BIGSERIAL               PRIMARY KEY,
    chain_id            BIGINT                  REFERENCES timetable.chain(chain_id) ON UPDATE CASCADE ON DELETE CASCADE,
    task_order          DOUBLE PRECISION        NOT NULL,
    task_name           TEXT,
    kind                timetable.command_kind  NOT NULL DEFAULT 'SQL',
    command             TEXT                    NOT NULL,
    run_as              TEXT,
    database_connection TEXT,
    ignore_error        BOOLEAN                 NOT NULL DEFAULT FALSE,
    autonomous          BOOLEAN                 NOT NULL DEFAULT FALSE,
    timeout             INTEGER                 DEFAULT 0,
    live                BOOLEAN                 NOT NULL DEFAULT TRUE
);          

COMMENT ON TABLE timetable.task IS
    'Holds information about chain elements aka tasks';
COMMENT ON COLUMN timetable.task.chain_id IS
    'Link to the chain, if NULL task considered to be disabled';
COMMENT ON COLUMN timetable.task.task_order IS
    'Indicates the order of task within a chain';    
COMMENT ON COLUMN timetable.task.run_as IS
    'Role name to run task as. Uses SET ROLE for SQL commands';
COMMENT ON COLUMN timetable.task.ignore_error IS
    'Indicates whether a next task in a chain can be executed regardless of the success of the current one';
COMMENT ON COLUMN timetable.task.kind IS
    'Indicates whether "command" is SQL, built-in function or an external program';
COMMENT ON COLUMN timetable.task.command IS
    'Contains either an SQL command, or command string to be executed';
COMMENT ON COLUMN timetable.task.timeout IS
    'Abort any task within a chain that takes more than the specified number of milliseconds';
COMMENT ON COLUMN timetable.task.autonomous IS
    'Specify if the task should be executed out of the chain transaction. Useful for VACUUM, CREATE DATABASE, CALL etc.';
COMMENT ON COLUMN timetable.task.live IS
    'Indication that the task is ready to run, set to FALSE to skip execution';

-- parameter passing for a chain task
CREATE TABLE timetable.parameter(
    task_id     BIGINT  REFERENCES timetable.task(task_id)
                        ON UPDATE CASCADE ON DELETE CASCADE,
    order_id    INTEGER CHECK (order_id > 0),
    value       JSONB,
    PRIMARY KEY (task_id, order_id)
);

COMMENT ON TABLE timetable.parameter IS
    'Stores parameters passed as arguments to a chain task';

CREATE UNLOGGED TABLE timetable.active_session(
    client_pid  BIGINT  NOT NULL,
    server_pid  BIGINT  NOT NULL,
    client_name TEXT    NOT NULL,
    started_at  TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE timetable.active_session IS
    'Stores information about active sessions';

CREATE TYPE timetable.log_type AS ENUM ('DEBUG', 'NOTICE', 'INFO', 'ERROR', 'PANIC', 'USER');

CREATE OR REPLACE FUNCTION timetable.get_client_name(integer) RETURNS TEXT AS
$$
    SELECT client_name FROM timetable.active_session WHERE server_pid = $1 LIMIT 1
$$
LANGUAGE sql;

CREATE TABLE timetable.log
(
    ts              TIMESTAMPTZ         DEFAULT now(),
    pid             INTEGER             NOT NULL,
    log_level       timetable.log_type  NOT NULL,
    client_name     TEXT                DEFAULT timetable.get_client_name(pg_backend_pid()),
    message         TEXT,
    message_data    jsonb
);

COMMENT ON TABLE timetable.log IS
    'Stores log entries of active sessions';

CREATE TABLE timetable.execution_log (
    chain_id        BIGINT,
    task_id         BIGINT,
    txid            BIGINT NOT NULL,
    last_run        TIMESTAMPTZ DEFAULT now(),
    finished        TIMESTAMPTZ,
    pid             BIGINT,
    returncode      INTEGER,
    ignore_error    BOOLEAN,
    kind            timetable.command_kind,
    command         TEXT,
    output          TEXT,
    client_name     TEXT        NOT NULL,
    params          TEXT
);

COMMENT ON TABLE timetable.execution_log IS
    'Stores log entries of executed tasks and chains';
COMMENT ON COLUMN timetable.execution_log.chain_id IS
    'Link to the chain executed';
COMMENT ON COLUMN timetable.execution_log.task_id IS
    'Link to the task executed';
COMMENT ON COLUMN timetable.execution_log.txid IS
    'Transaction ID of the executed task';
COMMENT ON COLUMN timetable.execution_log.last_run IS
    'Timestamp of the last execution of the task';
COMMENT ON COLUMN timetable.execution_log.finished IS
    'Timestamp of the task execution finish';
COMMENT ON COLUMN timetable.execution_log.pid IS
    'Process ID of the worker executing the task';
COMMENT ON COLUMN timetable.execution_log.returncode IS
    'Return code of the executed task';
COMMENT ON COLUMN timetable.execution_log.ignore_error IS
    'Indicates whether a next task in a chain can be executed regardless of the success of the current one';
COMMENT ON COLUMN timetable.execution_log.kind IS
    'Indicates whether "command" is SQL, built-in function or an external program';
COMMENT ON COLUMN timetable.execution_log.command IS
    'Contains either an SQL command, or command string to be executed';
COMMENT ON COLUMN timetable.execution_log.output IS
    'Contains output of the executed task';
COMMENT ON COLUMN timetable.execution_log.client_name IS
    'Name of the client executing the task';
COMMENT ON COLUMN timetable.execution_log.params IS
    'Contains parameters passed as arguments to a chain task';

CREATE INDEX execution_log_chain_id_finished_idx
    ON timetable.execution_log (chain_id, finished);

CREATE INDEX execution_log_finished_brin_idx
    ON timetable.execution_log USING brin (finished);

CREATE UNLOGGED TABLE timetable.active_chain(
    chain_id    BIGINT  NOT NULL,
    client_name TEXT    NOT NULL,
    started_at  TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE timetable.active_chain IS
    'Stores information about active chains within session';

CREATE OR REPLACE FUNCTION timetable.try_lock_client_name(worker_pid BIGINT, worker_name TEXT)
RETURNS bool AS
$CODE$
BEGIN
    IF pg_is_in_recovery() THEN
        RAISE NOTICE 'Cannot obtain lock on a replica. Please, use the primary node';
        RETURN FALSE;
    END IF;
    -- remove disconnected sessions
    DELETE
        FROM timetable.active_session
        WHERE server_pid NOT IN (
            SELECT pid
            FROM pg_catalog.pg_stat_activity
            WHERE application_name = 'pg_timetable'
        );
    DELETE 
        FROM timetable.active_chain 
        WHERE client_name NOT IN (
            SELECT client_name FROM timetable.active_session
        );
    -- check if there any active sessions with the client name but different client pid
    PERFORM 1
        FROM timetable.active_session s
        WHERE
            s.client_pid <> worker_pid
            AND s.client_name = worker_name
        LIMIT 1;
    IF FOUND THEN
        RAISE NOTICE 'Another client is already connected to server with name: %', worker_name;
        RETURN FALSE;
    END IF;
    -- insert current session information
    INSERT INTO timetable.active_session(client_pid, client_name, server_pid) VALUES (worker_pid, worker_name, pg_backend_pid());
    RETURN TRUE;
END;
$CODE$
STRICT
LANGUAGE plpgsql;


-- 00798 Add timetable.secret store (mirrors migrations/00798.sql).
--
-- pg_timetable never installs any PostgreSQL extension. This block applies
-- unchanged on a database that has no pgcrypto installed. The extension is
-- looked up at call time inside resolve_secret.

CREATE TABLE timetable.secret (
    client_name  TEXT        NOT NULL,
    secret_name  TEXT        NOT NULL,
    value_enc    BYTEA       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT        NOT NULL DEFAULT session_user,
    PRIMARY KEY (client_name, secret_name)
);

ALTER TABLE timetable.secret ADD CONSTRAINT secret_name_format
    CHECK (secret_name ~ '^[A-Za-z0-9_.-]+$');

COMMENT ON TABLE timetable.secret IS
    'Write-only, named secret values referenced from task parameters and connection strings as ${secret:name}, scoped to exactly one client_name. Modeled on GitHub Actions repository secrets: no plaintext read path, no rotation, no versioning, no cross-client sharing. Requires the pgcrypto extension, which the database administrator installs; pg_timetable itself never does.';
COMMENT ON COLUMN timetable.secret.client_name IS
    'Owning client. Mandatory security boundary: resolvable only by the scheduler process running with this exact client_name (-c/--clientname). Unlike timetable.chain.client_name, NULL/global is not permitted.';
COMMENT ON COLUMN timetable.secret.secret_name IS
    'Reference key used in ${secret:name} syntax, unique within client_name. Case-sensitive, no whitespace (enforced by CHECK secret_name_format).';
COMMENT ON COLUMN timetable.secret.value_enc IS
    'Value encrypted by the operator with pgp_sym_encrypt() from the pgcrypto extension. Decrypted only by timetable.resolve_secret() when supplied the key configured as PGTT_SECRET_KEY, which the database never stores.';
COMMENT ON COLUMN timetable.secret.updated_by IS
    'session_user that last inserted or updated this row, maintained by the secret_touch trigger.';

REVOKE ALL ON timetable.secret FROM PUBLIC;
-- No grants to other roles. Objects are owned by the schema-creating (scheduler)
-- role; a separate administrative role is an operator-managed GRANT.

CREATE OR REPLACE FUNCTION timetable.secret_touch() RETURNS trigger AS
$CODE$
BEGIN
    NEW.updated_at := now();
    NEW.updated_by := session_user;
    RETURN NEW;
END;
$CODE$
LANGUAGE plpgsql;

COMMENT ON FUNCTION timetable.secret_touch() IS
    'Keeps timetable.secret.updated_at/updated_by truthful on UPDATE';

CREATE TRIGGER secret_touch
    BEFORE UPDATE ON timetable.secret
    FOR EACH ROW EXECUTE PROCEDURE timetable.secret_touch();

-- resolve_secret is LANGUAGE plpgsql so the validator does not resolve
-- pgp_sym_decrypt at create time; the extension schema is looked up at call
-- time and interpolated into dynamic SQL so any install layout (public,
-- custom schema, ALTER EXTENSION ... SET SCHEMA) works. A missing extension
-- raises SQLSTATE 0A000 without requiring pgcrypto for create time.
CREATE OR REPLACE FUNCTION timetable.resolve_secret(p_name TEXT, p_client TEXT, p_key TEXT)
RETURNS TEXT
LANGUAGE plpgsql
SECURITY DEFINER
STABLE
STRICT
SET search_path = pg_catalog, timetable
AS $CODE$
DECLARE
    v_enc        BYTEA;
    v_ext_schema TEXT;
    v_plain      TEXT;
BEGIN
    SELECT value_enc INTO v_enc
    FROM timetable.secret
    WHERE client_name = p_client
      AND secret_name = p_name;

    IF NOT FOUND THEN
        RETURN NULL;   -- unknown (client_name, secret_name): no pgcrypto needed
    END IF;

    SELECT n.nspname INTO v_ext_schema
    FROM pg_catalog.pg_extension e
    JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
    WHERE e.extname = 'pgcrypto';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'pgcrypto extension is not installed, cannot decrypt timetable.secret values'
            USING ERRCODE = 'feature_not_supported',
                  HINT = 'Install it (CREATE EXTENSION pgcrypto) or stop using ${secret:...} references';
    END IF;

    EXECUTE format('SELECT %I.pgp_sym_decrypt($1, $2)', v_ext_schema)
       INTO v_plain
      USING v_enc, p_key;

    RETURN v_plain;
END;
$CODE$;

COMMENT ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) IS
    'Returns the decrypted value of one secret, or NULL when the (client_name, secret_name) pair does not exist. Raises feature_not_supported when pgcrypto is not installed, and Wrong key or corrupt data when the key is wrong.';

REVOKE ALL ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) FROM PUBLIC;

CREATE OR REPLACE FUNCTION timetable.secret_count()
RETURNS BIGINT
LANGUAGE sql
SECURITY DEFINER
STABLE
SET search_path = pg_catalog, timetable
AS $$
    SELECT count(*) FROM timetable.secret;
$$;

COMMENT ON FUNCTION timetable.secret_count() IS
    'Number of stored secrets, used by the scheduler startup check when no encryption key is configured. Exposes no secret material and does not require pgcrypto.';

REVOKE ALL ON FUNCTION timetable.secret_count() FROM PUBLIC;
