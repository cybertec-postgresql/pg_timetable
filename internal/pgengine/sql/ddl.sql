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
    (0, '0051 Implement upgrade machinery'),
    (1, '0070 Interval scheduling and cron only syntax'),
    (2, '0086 Add task output to execution_log'),
    (3, '0108 Add client_name column to timetable.run_status'),
    (4, '0122 Add autonomous tasks'),
    (5, '0105 Add next_run function'),
    (6, '0149 Reimplement session locking'),
    (7, '0155 Rename SHELL task kind to PROGRAM'),
    (8, '0178 Disable tasks on a REPLICA node'),
    (9, '0195 Add notify_chain_start() and notify_chain_stop() functions');

CREATE TYPE timetable.command_kind AS ENUM ('SQL', 'PROGRAM', 'BUILTIN');

CREATE TABLE timetable.command (
    command_id  BIGSERIAL               PRIMARY KEY,
    name        TEXT                    NOT NULL    UNIQUE,
    kind        timetable.command_kind  NOT NULL    DEFAULT 'SQL',
    script      TEXT                    NOT NULL,
    CHECK (CASE WHEN kind <> 'BUILTIN' THEN script IS NOT NULL ELSE TRUE END)
);

COMMENT ON TABLE timetable.command IS
    'Commands pg_timetable actually knows about';
COMMENT ON COLUMN timetable.command.kind IS
    'Indicates whether "script" is SQL, built-in function or external program';
COMMENT ON COLUMN timetable.command.script IS
    'Contains either an SQL script, or command string to be executed';


CREATE TABLE timetable.task (
    task_id             BIGSERIAL   PRIMARY KEY,
    parent_id           BIGINT      UNIQUE  REFERENCES timetable.task(task_id)
                                    ON UPDATE CASCADE
                                    ON DELETE CASCADE,
    command_id          BIGINT      NOT NULL REFERENCES timetable.command(command_id)
                                    ON UPDATE CASCADE
                                    ON DELETE CASCADE,
    run_as              TEXT,
    database_connection TEXT,
    ignore_error        BOOLEAN     NOT NULL DEFAULT FALSE,
    autonomous          BOOLEAN     NOT NULL DEFAULT FALSE
);

COMMENT ON TABLE timetable.task IS
    'Holds information about chain elements aka tasks';
COMMENT ON COLUMN timetable.task.parent_id IS
    'Link to the parent task, if NULL task considered to be head of a chain';
COMMENT ON COLUMN timetable.task.run_as IS
    'Role name to run task as. Uses SET ROLE for SQL commands';
COMMENT ON COLUMN timetable.task.ignore_error IS
    'Indicates whether a next task in a chain can be executed regardless of the success of the current one';

CREATE DOMAIN timetable.cron AS TEXT CHECK(
    substr(VALUE, 1, 6) IN ('@every', '@after') AND (substr(VALUE, 7) :: INTERVAL) IS NOT NULL
    OR VALUE = '@reboot'
    OR VALUE ~ '^(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) +){4}(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) ?)$'
);

COMMENT ON DOMAIN timetable.cron IS 'Extended CRON-style notation with support of interval values';

CREATE TABLE timetable.chain (
    chain_id            BIGSERIAL   PRIMARY KEY,
    task_id             BIGINT      REFERENCES timetable.task(task_id)
                                            ON UPDATE CASCADE
                                            ON DELETE CASCADE,
    chain_name          TEXT        NOT NULL UNIQUE,
    run_at              timetable.cron,
    max_instances       INTEGER,
    live                BOOLEAN     DEFAULT FALSE,
    self_destruct       BOOLEAN     DEFAULT FALSE,
    exclusive_execution BOOLEAN     DEFAULT FALSE,
    client_name         TEXT
);

COMMENT ON TABLE timetable.chain IS
    'Stores information about chains schedule';
COMMENT ON COLUMN timetable.chain.task_id IS
    'First task (head) of the chain';
COMMENT ON COLUMN timetable.chain.run_at IS
    'Extended CRON-style time notation the chain has to be run at';
COMMENT ON COLUMN timetable.chain.max_instances IS
    'Number of instances (clients) this chain can run in parallel';
COMMENT ON COLUMN timetable.chain.live IS
    'Indication that the chain is ready to run, set to FALSE to pause execution';
COMMENT ON COLUMN timetable.chain.self_destruct IS
    'Indication that this chain will delete itself after successful run';
COMMENT ON COLUMN timetable.chain.exclusive_execution IS
    'All parallel chains should be paused while executing this chain';
COMMENT ON COLUMN timetable.chain.client_name IS
    'Only client with this name is allowed to run this chain, set to NULL to allow any client';

-- parameter passing for a chain task
CREATE TABLE timetable.parameter(
    chain_id    BIGINT  REFERENCES timetable.chain (chain_id)
                                    ON UPDATE CASCADE
                                    ON DELETE CASCADE,
    task_id     BIGINT  REFERENCES timetable.task(task_id)
                                    ON UPDATE CASCADE
                                    ON DELETE CASCADE,
    order_id    INTEGER CHECK (order_id > 0),
    value       jsonb,
    PRIMARY KEY (chain_id, task_id, order_id)
);

COMMENT ON TABLE timetable.parameter IS
    'Stores parameters passed as arguments to a chain task';

CREATE UNLOGGED TABLE timetable.active_session(
    client_pid BIGINT NOT NULL,
    client_name TEXT NOT NULL,
    server_pid BIGINT NOT NULL
);

COMMENT ON TABLE timetable.active_session IS
    'Stores information about active sessions';


CREATE TYPE timetable.log_type AS ENUM ('DEBUG', 'NOTICE', 'LOG', 'ERROR', 'PANIC', 'USER');

CREATE OR REPLACE FUNCTION timetable.get_client_name(integer) RETURNS TEXT AS
$$
    SELECT client_name FROM timetable.active_session WHERE server_pid = $1 LIMIT 1
$$
LANGUAGE sql;

CREATE TABLE timetable.log
(
    ts              TIMESTAMPTZ         DEFAULT now(),
    client_name     TEXT                DEFAULT timetable.get_client_name(pg_backend_pid()),
    pid             INTEGER             NOT NULL,
    log_level       timetable.log_type  NOT NULL,
    message         TEXT,
    message_data    jsonb
);

-- log timetable related action
CREATE TABLE timetable.execution_log (
    chain_id    BIGINT,
    task_id     BIGINT,
    command_id  BIGINT,
    name        TEXT        NOT NULL,
    script      TEXT,
    kind        TEXT,
    last_run    TIMESTAMPTZ DEFAULT now(),
    finished    TIMESTAMPTZ,
    returncode  INTEGER,
    pid         BIGINT,
    output      TEXT,
    client_name TEXT        NOT NULL
);

CREATE TYPE timetable.execution_status AS ENUM ('STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'DEAD');

CREATE TABLE timetable.run_status (
    run_status                  BIGSERIAL   PRIMARY KEY,
    start_status                BIGINT,
    execution_status            timetable.execution_status,
    task_id                     BIGINT,
    current_execution_element   BIGINT,
    started                     TIMESTAMPTZ,
    last_status_update          TIMESTAMPTZ DEFAULT clock_timestamp(),
    chain_id                    BIGINT,
    client_name                 TEXT        NOT NULL
);

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
    INSERT INTO timetable.active_session VALUES (worker_pid, worker_name, pg_backend_pid());
    RETURN TRUE;
END;
$CODE$
STRICT
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION timetable.health_check(client_name TEXT)
RETURNS void AS
$CODE$
    INSERT INTO timetable.run_status
        (execution_status, started, last_status_update, start_status, chain_id, client_name)
    SELECT 'DEAD', now(), now(), start_status, 0, $1 FROM (
        SELECT   start_status
        FROM   timetable.run_status
        WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'DEAD') AND client_name = $1
        GROUP BY 1
        HAVING count(*) < 2 ) AS abc
$CODE$
STRICT LANGUAGE sql;

CREATE OR REPLACE FUNCTION timetable.trig_chain_fixer() RETURNS trigger AS $$
    DECLARE
        tmp_parent_id BIGINT;
        tmp_task_id BIGINT;
        orig_task_id BIGINT;
        tmp_chain_head_id BIGINT;
        i BIGINT;
    BEGIN
        --raise notice 'Fixing chain for deletion of command#%', OLD.command_id;
        FOR orig_task_id IN
            SELECT task_id FROM timetable.task WHERE command_id = OLD.command_id
        LOOP
            --raise notice 'task_id#%', orig_task_id;
            tmp_task_id := orig_task_id;
            i := 0;
            LOOP
                i := i + 1;
                SELECT parent_id INTO tmp_parent_id FROM timetable.task
                    WHERE task_id = tmp_task_id;
                EXIT WHEN tmp_parent_id IS NULL;
                IF i > 100 THEN
                    RAISE EXCEPTION 'Infinite loop at timetable.task.task_id=%', tmp_task_id;
                    RETURN NULL;
                END IF;
                tmp_task_id := tmp_parent_id;
            END LOOP;

            SELECT parent_id INTO tmp_chain_head_id FROM timetable.task
                WHERE task_id = tmp_task_id;
            --raise notice 'PERFORM task_delete(%,%)', tmp_chain_head_id, orig_task_id;
            PERFORM timetable.task_delete(tmp_chain_head_id, orig_task_id);
        END LOOP;
        RETURN OLD;
    END;
$$ LANGUAGE 'plpgsql';

CREATE TRIGGER trig_task_fixer
        BEFORE DELETE ON timetable.command
        FOR EACH ROW EXECUTE PROCEDURE timetable.trig_chain_fixer();

CREATE OR REPLACE FUNCTION timetable.task_delete(config_ bigint, task_id_ bigint) RETURNS boolean AS $$
DECLARE
        task_id_1st_    bigint;
        id_in_chain     bool;
        task_id_curs    bigint;
        task_id_before  bigint;
        task_id_after   bigint;
        curs1           refcursor;
BEGIN
        SELECT task_id INTO task_id_1st_ FROM timetable.chain WHERE chain_id = config_;
        -- No such chain_id
        IF NOT FOUND THEN
                RAISE NOTICE 'No such chain_id';
                RETURN false;
        END IF;
        -- This head is not connected to a chain
        IF task_id_1st_ IS NULL THEN
                RAISE NOTICE 'This head is not connected to a chain';
                RETURN false;
        END IF;

        OPEN curs1 FOR WITH RECURSIVE x (task_id) AS (
                SELECT task_id FROM timetable.task
                WHERE task_id = task_id_1st_ AND parent_id IS NULL
                UNION ALL
                SELECT timetable.task.task_id FROM timetable.task, x
                WHERE timetable.task.parent_id = x.task_id
        ) SELECT task_id FROM x;

        id_in_chain = false;
        task_id_curs = NULL;
        task_id_before = NULL;
        task_id_after = NULL;
        LOOP
                FETCH curs1 INTO task_id_curs;
                IF id_in_chain = false AND task_id_curs <> task_id_ THEN
                        task_id_before = task_id_curs;
                END IF;
                IF task_id_curs = task_id_ THEN
                        id_in_chain = true;
                END IF;
                EXIT WHEN id_in_chain OR NOT FOUND;
        END LOOP;

        IF id_in_chain THEN
                FETCH curs1 INTO task_id_after;
        ELSE
                CLOSE curs1;
                RAISE NOTICE 'This task_id is not part of chain pointed by the chain_id';
                RETURN false;
        END IF;

        CLOSE curs1;

        IF task_id_before IS NULL THEN
            UPDATE timetable.chain SET task_id = task_id_after WHERE chain_id = config_;
        END IF;
        UPDATE timetable.task SET parent_id = NULL WHERE task_id = task_id_;
        UPDATE timetable.task SET parent_id = task_id_before WHERE task_id = task_id_after;
        DELETE FROM timetable.task WHERE task_id = task_id_;

        RETURN true;
END
$$ LANGUAGE plpgsql;