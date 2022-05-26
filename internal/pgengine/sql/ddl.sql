CREATE SCHEMA timetable;

-- define migrations you need to apply
-- every change to this file should populate this table.
-- Version value should contain issue number zero padded followed by
-- short description of the issue\feature\bug implemented\resolved
CREATE TABLE timetable.migration(
    id INT8 NOT NULL,
    version TEXT NOT NULL,
    PRIMARY KEY (id)
);

INSERT INTO
    timetable.migration (id, version)
VALUES
    (0, '00259 Restart migrations for v4'),
    (1, '00305 Fix timetable.is_cron_in_time'),
    (2, '00323 Append timetable.delete_job function'),
    (3, '00329 Migration required for some new added functions'),
    (4, '00334 Refactor timetable.task as plain schema without tree-like dependencies'),
    (5, '00381 Rewrite active chain handling'),
    (6, '00394 Add started_at column to active_session and active_chain tables'),
    (7, '00417 Rename LOG database log level to INFO'),
    (8, '00436 Add txid column to timetable.execution_log');

CREATE DOMAIN timetable.cron AS TEXT CHECK(
    substr(VALUE, 1, 6) IN ('@every', '@after') AND (substr(VALUE, 7) :: INTERVAL) IS NOT NULL
    OR VALUE = '@reboot'
    OR VALUE ~ '^(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) +){4}(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) ?)$'
);

COMMENT ON DOMAIN timetable.cron IS 'Extended CRON-style notation with support of interval values';

CREATE TABLE timetable.chain (
    chain_id            BIGSERIAL   PRIMARY KEY,
    chain_name          TEXT        NOT NULL UNIQUE,
    run_at              timetable.cron,
    max_instances       INTEGER,
    timeout             INTEGER     DEFAULT 0,
    live                BOOLEAN     DEFAULT FALSE,
    self_destruct       BOOLEAN     DEFAULT FALSE,
    exclusive_execution BOOLEAN     DEFAULT FALSE,
    client_name         TEXT
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
    timeout             INTEGER                 DEFAULT 0
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
    client_name TEXT    NOT NULL,
    server_pid  BIGINT  NOT NULL,
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
    chain_id    BIGINT,
    task_id     BIGINT,
    txid        INTEGER NOT NULL,
    last_run    TIMESTAMPTZ DEFAULT now(),
    finished    TIMESTAMPTZ,
    pid         BIGINT,
    returncode  INTEGER,
    kind        timetable.command_kind,
    command     TEXT,
    output      TEXT,
    client_name TEXT        NOT NULL
);

COMMENT ON TABLE timetable.execution_log IS
    'Stores log entries of executed tasks and chains';

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

