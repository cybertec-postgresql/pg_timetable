CREATE SCHEMA timetable;

CREATE TYPE timetable.command_kind AS ENUM ('SQL', 'PROGRAM', 'BUILTIN');

CREATE TABLE timetable.task (
    task_id             BIGSERIAL   PRIMARY KEY,
    parent_id           BIGINT      UNIQUE      REFERENCES timetable.task(task_id)
                                                ON UPDATE CASCADE ON DELETE CASCADE,
    task_name           TEXT,
    kind                timetable.command_kind  NOT NULL DEFAULT 'SQL',
    command             TEXT                    NOT NULL,
    run_as              TEXT,
    database_connection TEXT,
    ignore_error        BOOLEAN                 NOT NULL DEFAULT FALSE,
    autonomous          BOOLEAN                 NOT NULL DEFAULT FALSE
);          

COMMENT ON TABLE timetable.task IS
    'Holds information about chain elements aka tasks';
COMMENT ON COLUMN timetable.task.parent_id IS
    'Link to the parent task, if NULL task considered to be head of a chain';
COMMENT ON COLUMN timetable.task.run_as IS
    'Role name to run task as. Uses SET ROLE for SQL commands';
COMMENT ON COLUMN timetable.task.ignore_error IS
    'Indicates whether a next task in a chain can be executed regardless of the success of the current one';
COMMENT ON COLUMN timetable.task.kind IS
    'Indicates whether "command" is SQL, built-in function or an external program';
COMMENT ON COLUMN timetable.task.command IS
    'Contains either an SQL command, or command string to be executed';

CREATE DOMAIN timetable.cron AS TEXT CHECK(
    substr(VALUE, 1, 6) IN ('@every', '@after') AND (substr(VALUE, 7) :: INTERVAL) IS NOT NULL
    OR VALUE = '@reboot'
    OR VALUE ~ '^(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) +){4}(((\d+,)+\d+|(\d+(\/|-)\d+)|(\*(\/|-)\d+)|\d+|\*) ?)$'
);

COMMENT ON DOMAIN timetable.cron IS 'Extended CRON-style notation with support of interval values';

CREATE TABLE timetable.chain (
    chain_id            BIGSERIAL   PRIMARY KEY,
    task_id             BIGINT      REFERENCES timetable.task(task_id)
                                    ON UPDATE CASCADE ON DELETE CASCADE,
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
                        ON UPDATE CASCADE ON DELETE CASCADE,
    task_id     BIGINT  REFERENCES timetable.task(task_id)
                        ON UPDATE CASCADE ON DELETE CASCADE,
    order_id    INTEGER CHECK (order_id > 0),
    value       JSONB,
    PRIMARY KEY (chain_id, task_id, order_id)
);

COMMENT ON TABLE timetable.parameter IS
    'Stores parameters passed as arguments to a chain task';

CREATE UNLOGGED TABLE timetable.active_session(
    client_pid  BIGINT  NOT NULL,
    client_name TEXT    NOT NULL,
    server_pid  BIGINT  NOT NULL
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
    command     TEXT,
    kind        timetable.command_kind,
    last_run    TIMESTAMPTZ DEFAULT now(),
    finished    TIMESTAMPTZ,
    returncode  INTEGER,
    pid         BIGINT,
    output      TEXT,
    client_name TEXT        NOT NULL
);

CREATE TYPE timetable.execution_status AS ENUM ('CHAIN_STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'TASK_STARTED', 'TASK_DONE', 'DEAD');

CREATE TABLE timetable.run_status (
    run_status_id           BIGSERIAL   PRIMARY KEY,
    start_status_id         BIGINT      REFERENCES timetable.run_status(run_status_id)
                                        ON UPDATE CASCADE ON DELETE CASCADE,
    execution_status        timetable.execution_status,
    chain_id                BIGINT,
    task_id                 BIGINT,
    created_at              TIMESTAMPTZ DEFAULT clock_timestamp(),
    client_name             TEXT        NOT NULL
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

CREATE OR REPLACE FUNCTION timetable.cron_split_to_arrays(
    cron text,
    OUT mins integer[],
    OUT hours integer[],
    OUT days integer[],
    OUT months integer[],
    OUT dow integer[]
) RETURNS record AS $$
DECLARE
    a_element text[];
    i_index integer;
    a_tmp text[];
    tmp_item text;
    a_range int[];
    a_split text[];
    a_res integer[];
    allowed_range integer[];
    max_val integer;
    min_val integer;
BEGIN
    a_element := regexp_split_to_array(cron, '\s+');
    FOR i_index IN 1..5 LOOP
        a_res := NULL;
        a_tmp := string_to_array(a_element[i_index],',');
        CASE i_index -- 1 - mins, 2 - hours, 3 - days, 4 - weeks, 5 - DOWs
            WHEN 1 THEN allowed_range := '{0,59}';
            WHEN 2 THEN allowed_range := '{0,23}';
            WHEN 3 THEN allowed_range := '{1,31}';
            WHEN 4 THEN allowed_range := '{1,12}';
        ELSE
            allowed_range := '{0,7}';
        END CASE;
        FOREACH  tmp_item IN ARRAY a_tmp LOOP
            IF tmp_item ~ '^[0-9]+$' THEN -- normal integer
                a_res := array_append(a_res, tmp_item::int);
            ELSIF tmp_item ~ '^[*]+$' THEN -- '*' any value
                a_range := array(select generate_series(allowed_range[1], allowed_range[2]));
                a_res := array_cat(a_res, a_range);
            ELSIF tmp_item ~ '^[0-9]+[-][0-9]+$' THEN -- '-' range of values
                a_range := regexp_split_to_array(tmp_item, '-');
                a_range := array(select generate_series(a_range[1], a_range[2]));
                a_res := array_cat(a_res, a_range);
            ELSIF tmp_item ~ '^[0-9]+[\/][0-9]+$' THEN -- '/' step values
                a_range := regexp_split_to_array(tmp_item, '/');
                a_range := array(select generate_series(a_range[1], allowed_range[2], a_range[2]));
                a_res := array_cat(a_res, a_range);
            ELSIF tmp_item ~ '^[0-9-]+[\/][0-9]+$' THEN -- '-' range of values and '/' step values
                a_split := regexp_split_to_array(tmp_item, '/');
                a_range := regexp_split_to_array(a_split[1], '-');
                a_range := array(select generate_series(a_range[1], a_range[2], a_split[2]::int));
                a_res := array_cat(a_res, a_range);
            ELSIF tmp_item ~ '^[*]+[\/][0-9]+$' THEN -- '*' any value and '/' step values
                a_split := regexp_split_to_array(tmp_item, '/');
                a_range := array(select generate_series(allowed_range[1], allowed_range[2], a_split[2]::int));
                a_res := array_cat(a_res, a_range);
            ELSE
                RAISE EXCEPTION 'Value ("%") not recognized', a_element[i_index]
                    USING HINT = 'fields separated by space or tab.'+
                       'Values allowed: numbers (value list with ","), '+
                    'any value with "*", range of value with "-" and step values with "/"!';
            END IF;
        END LOOP;
        SELECT
           ARRAY_AGG(x.val), MIN(x.val), MAX(x.val) INTO a_res, min_val, max_val
        FROM (
            SELECT DISTINCT UNNEST(a_res) AS val ORDER BY val) AS x;
        IF max_val > allowed_range[2] OR min_val < allowed_range[1] THEN
            RAISE EXCEPTION '% is out of range: %', a_res, allowed_range;
        END IF;
        CASE i_index
                  WHEN 1 THEN mins := a_res;
            WHEN 2 THEN hours := a_res;
            WHEN 3 THEN days := a_res;
            WHEN 4 THEN months := a_res;
        ELSE
            dow := a_res;
        END CASE;
    END LOOP;
    RETURN;
END;
$$ LANGUAGE PLPGSQL STRICT;

CREATE OR REPLACE FUNCTION timetable.cron_months(
    from_ts timestamptz,
    allowed_months int[]
) RETURNS SETOF timestamptz AS $$
    WITH
    am(am) AS (SELECT UNNEST(allowed_months)),
    genm(ts) AS ( --generated months
        SELECT date_trunc('month', ts)
        FROM pg_catalog.generate_series(from_ts, from_ts + INTERVAL '1 year', INTERVAL '1 month') g(ts)
    )
    SELECT ts FROM genm JOIN am ON date_part('month', genm.ts) = am.am
$$ LANGUAGE SQL STRICT;

CREATE OR REPLACE FUNCTION timetable.cron_days(
    from_ts timestamptz,
    allowed_months int[],
    allowed_days int[],
    allowed_week_days int[]
) RETURNS SETOF timestamptz AS $$
    WITH
    ad(ad) AS (SELECT UNNEST(allowed_days)),
    am(am) AS (SELECT * FROM timetable.cron_months(from_ts, allowed_months)),
    gend(ts) AS ( --generated days
        SELECT date_trunc('day', ts)
        FROM am,
            pg_catalog.generate_series(am.am, am.am + INTERVAL '1 month'
                - INTERVAL '1 day',  -- don't include the same day of the next month
                INTERVAL '1 day') g(ts)
    )
    SELECT ts
    FROM gend JOIN ad ON date_part('day', gend.ts) = ad.ad
    WHERE extract(dow from ts)=ANY(allowed_week_days)
$$ LANGUAGE SQL STRICT;

CREATE OR REPLACE FUNCTION timetable.cron_times(
    allowed_hours int[],
    allowed_minutes int[]
) RETURNS SETOF time AS $$
    WITH
    ah(ah) AS (SELECT UNNEST(allowed_hours)),
    am(am) AS (SELECT UNNEST(allowed_minutes))
    SELECT make_time(ah.ah, am.am, 0) FROM ah CROSS JOIN am
$$ LANGUAGE SQL STRICT;


CREATE OR REPLACE FUNCTION timetable.cron_runs(
    from_ts timestamp with time zone, 
    cron text
) RETURNS SETOF timestamptz AS $$
    SELECT cd + ct
    FROM
        timetable.cron_split_to_arrays(cron) a,
        timetable.cron_times(a.hours, a.mins) ct CROSS JOIN
        timetable.cron_days(from_ts, a.months, a.days, a.dow) cd
    WHERE cd + ct > from_ts
    ORDER BY 1 ASC;
$$ LANGUAGE SQL STRICT;

-- is_cron_in_time returns TRUE if timestamp is listed in cron expression
CREATE OR REPLACE FUNCTION timetable.is_cron_in_time(
    run_at timetable.cron, 
    ts timestamptz
) RETURNS BOOLEAN AS $$
    SELECT
    CASE WHEN run_at IS NULL THEN
        TRUE
    ELSE
        date_part('month', ts) = ANY(a.months)
        AND date_part('dow', ts) = ANY(a.dow) OR date_part('isodow', ts) = ANY(a.dow)
        AND date_part('day', ts) = ANY(a.days)
        AND date_part('hour', ts) = ANY(a.hours)
        AND date_part('minute', ts) = ANY(a.mins)
    END
    FROM
        timetable.cron_split_to_arrays(run_at) a
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.next_run(cron timetable.cron) RETURNS timestamptz AS $$
    SELECT * FROM timetable.cron_runs(now(), cron) LIMIT 1
$$ LANGUAGE SQL STRICT;

CREATE OR REPLACE FUNCTION timetable.get_chain_running_statuses(chain_id BIGINT) RETURNS SETOF BIGINT AS $$
    SELECT  start_status.run_status_id 
    FROM    timetable.run_status start_status
    WHERE   start_status.execution_status = 'CHAIN_STARTED' 
            AND start_status.chain_id = $1
            AND NOT EXISTS (
                SELECT 1
                FROM    timetable.run_status finish_status
                WHERE   start_status.run_status_id = finish_status.start_status_id
                        AND finish_status.execution_status IN ('CHAIN_FAILED', 'CHAIN_DONE', 'DEAD')
            )
    ORDER BY 1
$$ LANGUAGE SQL STRICT;

COMMENT ON FUNCTION timetable.get_chain_running_statuses(chain_id BIGINT) IS
    'Returns a set of active run status IDs for a given chain';

CREATE OR REPLACE FUNCTION timetable.health_check(client_name TEXT) RETURNS void AS $$
    INSERT INTO timetable.run_status
        (execution_status, start_status_id, client_name)
    SELECT 
        'DEAD', start_status.run_status_id, $1 
        FROM    timetable.run_status start_status
        WHERE   start_status.execution_status = 'CHAIN_STARTED' 
            AND start_status.client_name = $1
            AND NOT EXISTS (
                SELECT 1
                FROM    timetable.run_status finish_status
                WHERE   start_status.run_status_id = finish_status.start_status_id
                        AND finish_status.execution_status IN ('CHAIN_FAILED', 'CHAIN_DONE', 'DEAD')
            )
$$ LANGUAGE SQL STRICT; 

CREATE OR REPLACE FUNCTION timetable.add_task(
    IN kind timetable.command_kind,
    IN command TEXT, 
    IN parent_id BIGINT
) RETURNS BIGINT AS $$
    INSERT INTO timetable.task (parent_id, kind, command) VALUES (parent_id, kind, command) RETURNING task_id
$$ LANGUAGE SQL;

-- add_job() will add one-task chain to the system
CREATE OR REPLACE FUNCTION timetable.add_job(
    job_name            TEXT,
    job_schedule        timetable.cron,
    job_command         TEXT,
    job_parameters      JSONB DEFAULT NULL,
    job_kind            timetable.command_kind DEFAULT 'SQL'::timetable.command_kind,
    job_client_name     TEXT DEFAULT NULL,
    job_max_instances   INTEGER DEFAULT NULL,
    job_live            BOOLEAN DEFAULT TRUE,
    job_self_destruct   BOOLEAN DEFAULT FALSE,
    job_ignore_errors   BOOLEAN DEFAULT TRUE
) RETURNS BIGINT AS $$
    WITH 
        cte_task(v_task_id) AS (
            INSERT INTO timetable.task (kind, command, ignore_error, autonomous)
            VALUES (job_kind, job_command, job_ignore_errors, TRUE)
            RETURNING task_id
        ),
        cte_chain (v_chain_id) AS (
            INSERT INTO timetable.chain (task_id, chain_name, run_at, max_instances, live,self_destruct, client_name) 
            SELECT v_task_id, job_name, job_schedule,job_max_instances, job_live, job_self_destruct, job_client_name
            FROM cte_task
            RETURNING chain_id
        )
        INSERT INTO timetable.parameter (chain_id, task_id, order_id, value)
        SELECT v_chain_id, v_task_id, 1, job_parameters FROM cte_task, cte_chain
        RETURNING chain_id
$$ LANGUAGE SQL STRICT;

CREATE OR REPLACE FUNCTION timetable.notify_chain_start(
    chain_id BIGINT, 
    worker_name TEXT
) RETURNS void AS $$
  SELECT pg_notify(
      worker_name, 
    format('{"ConfigID": %s, "Command": "START", "Ts": %s}', 
        chain_id, 
        EXTRACT(epoch FROM clock_timestamp())::bigint)
    )
$$ LANGUAGE SQL;

CREATE OR REPLACE FUNCTION timetable.notify_chain_stop(
    chain_id BIGINT, 
    worker_name TEXT
) RETURNS void AS  $$ 
  SELECT pg_notify(
      worker_name, 
    format('{"ConfigID": %s, "Command": "STOP", "Ts": %s}', 
        chain_id, 
        EXTRACT(epoch FROM clock_timestamp())::bigint)
    )
$$ LANGUAGE SQL;

-- json validation from:
-- https://github.com/gavinwahl/postgres-json-schema

CREATE OR REPLACE FUNCTION timetable._validate_json_schema_type(type text, data jsonb)
RETURNS boolean AS $$
BEGIN
  IF type = 'integer' THEN
    IF jsonb_typeof(data) != 'number' THEN
      RETURN false;
    END IF;
    IF trunc(data::text::numeric) != data::text::numeric THEN
      RETURN false;
    END IF;
  ELSE
    IF type != jsonb_typeof(data) THEN
      RETURN false;
    END IF;
  END IF;
  RETURN true;
END;
$$
LANGUAGE 'plpgsql' IMMUTABLE;


CREATE OR REPLACE FUNCTION timetable.validate_json_schema(schema jsonb, data jsonb, root_schema jsonb DEFAULT NULL)
RETURNS boolean AS $$
DECLARE
  prop text;
  item jsonb;
  path text[];
  types text[];
  pattern text;
  props text[];
BEGIN

  IF root_schema IS NULL THEN
    root_schema = schema;
  END IF;

  IF schema ? 'type' THEN
    IF jsonb_typeof(schema->'type') = 'array' THEN
      types = ARRAY(SELECT jsonb_array_elements_text(schema->'type'));
    ELSE
      types = ARRAY[schema->>'type'];
    END IF;
    IF (SELECT NOT bool_or(timetable._validate_json_schema_type(type, data)) FROM unnest(types) type) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'properties' THEN
    FOR prop IN SELECT jsonb_object_keys(schema->'properties') LOOP
      IF data ? prop AND NOT timetable.validate_json_schema(schema->'properties'->prop, data->prop, root_schema) THEN
        RETURN false;
      END IF;
    END LOOP;
  END IF;

  IF schema ? 'required' AND jsonb_typeof(data) = 'object' THEN
    IF NOT ARRAY(SELECT jsonb_object_keys(data)) @>
           ARRAY(SELECT jsonb_array_elements_text(schema->'required')) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'items' AND jsonb_typeof(data) = 'array' THEN
    IF jsonb_typeof(schema->'items') = 'object' THEN
      FOR item IN SELECT jsonb_array_elements(data) LOOP
        IF NOT timetable.validate_json_schema(schema->'items', item, root_schema) THEN
          RETURN false;
        END IF;
      END LOOP;
    ELSE
      IF NOT (
        SELECT bool_and(i > jsonb_array_length(schema->'items') OR timetable.validate_json_schema(schema->'items'->(i::int - 1), elem, root_schema))
        FROM jsonb_array_elements(data) WITH ORDINALITY AS t(elem, i)
      ) THEN
        RETURN false;
      END IF;
    END IF;
  END IF;

  IF jsonb_typeof(schema->'additionalItems') = 'boolean' and NOT (schema->'additionalItems')::text::boolean AND jsonb_typeof(schema->'items') = 'array' THEN
    IF jsonb_array_length(data) > jsonb_array_length(schema->'items') THEN
      RETURN false;
    END IF;
  END IF;

  IF jsonb_typeof(schema->'additionalItems') = 'object' THEN
    IF NOT (
        SELECT bool_and(timetable.validate_json_schema(schema->'additionalItems', elem, root_schema))
        FROM jsonb_array_elements(data) WITH ORDINALITY AS t(elem, i)
        WHERE i > jsonb_array_length(schema->'items')
      ) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'minimum' AND jsonb_typeof(data) = 'number' THEN
    IF data::text::numeric < (schema->>'minimum')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'maximum' AND jsonb_typeof(data) = 'number' THEN
    IF data::text::numeric > (schema->>'maximum')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF COALESCE((schema->'exclusiveMinimum')::text::bool, FALSE) THEN
    IF data::text::numeric = (schema->>'minimum')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF COALESCE((schema->'exclusiveMaximum')::text::bool, FALSE) THEN
    IF data::text::numeric = (schema->>'maximum')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'anyOf' THEN
    IF NOT (SELECT bool_or(timetable.validate_json_schema(sub_schema, data, root_schema)) FROM jsonb_array_elements(schema->'anyOf') sub_schema) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'allOf' THEN
    IF NOT (SELECT bool_and(timetable.validate_json_schema(sub_schema, data, root_schema)) FROM jsonb_array_elements(schema->'allOf') sub_schema) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'oneOf' THEN
    IF 1 != (SELECT COUNT(*) FROM jsonb_array_elements(schema->'oneOf') sub_schema WHERE timetable.validate_json_schema(sub_schema, data, root_schema)) THEN
      RETURN false;
    END IF;
  END IF;

  IF COALESCE((schema->'uniqueItems')::text::boolean, false) THEN
    IF (SELECT COUNT(*) FROM jsonb_array_elements(data)) != (SELECT count(DISTINCT val) FROM jsonb_array_elements(data) val) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'additionalProperties' AND jsonb_typeof(data) = 'object' THEN
    props := ARRAY(
      SELECT key
      FROM jsonb_object_keys(data) key
      WHERE key NOT IN (SELECT jsonb_object_keys(schema->'properties'))
        AND NOT EXISTS (SELECT * FROM jsonb_object_keys(schema->'patternProperties') pat WHERE key ~ pat)
    );
    IF jsonb_typeof(schema->'additionalProperties') = 'boolean' THEN
      IF NOT (schema->'additionalProperties')::text::boolean AND jsonb_typeof(data) = 'object' AND NOT props <@ ARRAY(SELECT jsonb_object_keys(schema->'properties')) THEN
        RETURN false;
      END IF;
    ELSEIF NOT (
      SELECT bool_and(timetable.validate_json_schema(schema->'additionalProperties', data->key, root_schema))
      FROM unnest(props) key
    ) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? '$ref' THEN
    path := ARRAY(
      SELECT regexp_replace(regexp_replace(path_part, '~1', '/'), '~0', '~')
      FROM UNNEST(regexp_split_to_array(schema->>'$ref', '/')) path_part
    );
    -- ASSERT path[1] = '#', 'only refs anchored at the root are supported';
    IF NOT timetable.validate_json_schema(root_schema #> path[2:array_length(path, 1)], data, root_schema) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'enum' THEN
    IF NOT EXISTS (SELECT * FROM jsonb_array_elements(schema->'enum') val WHERE val = data) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'minLength' AND jsonb_typeof(data) = 'string' THEN
    IF char_length(data #>> '{}') < (schema->>'minLength')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'maxLength' AND jsonb_typeof(data) = 'string' THEN
    IF char_length(data #>> '{}') > (schema->>'maxLength')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'not' THEN
    IF timetable.validate_json_schema(schema->'not', data, root_schema) THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'maxProperties' AND jsonb_typeof(data) = 'object' THEN
    IF (SELECT count(*) FROM jsonb_object_keys(data)) > (schema->>'maxProperties')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'minProperties' AND jsonb_typeof(data) = 'object' THEN
    IF (SELECT count(*) FROM jsonb_object_keys(data)) < (schema->>'minProperties')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'maxItems' AND jsonb_typeof(data) = 'array' THEN
    IF (SELECT count(*) FROM jsonb_array_elements(data)) > (schema->>'maxItems')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'minItems' AND jsonb_typeof(data) = 'array' THEN
    IF (SELECT count(*) FROM jsonb_array_elements(data)) < (schema->>'minItems')::numeric THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'dependencies' THEN
    FOR prop IN SELECT jsonb_object_keys(schema->'dependencies') LOOP
      IF data ? prop THEN
        IF jsonb_typeof(schema->'dependencies'->prop) = 'array' THEN
          IF NOT (SELECT bool_and(data ? dep) FROM jsonb_array_elements_text(schema->'dependencies'->prop) dep) THEN
            RETURN false;
          END IF;
        ELSE
          IF NOT timetable.validate_json_schema(schema->'dependencies'->prop, data, root_schema) THEN
            RETURN false;
          END IF;
        END IF;
      END IF;
    END LOOP;
  END IF;

  IF schema ? 'pattern' AND jsonb_typeof(data) = 'string' THEN
    IF (data #>> '{}') !~ (schema->>'pattern') THEN
      RETURN false;
    END IF;
  END IF;

  IF schema ? 'patternProperties' AND jsonb_typeof(data) = 'object' THEN
    FOR prop IN SELECT jsonb_object_keys(data) LOOP
      FOR pattern IN SELECT jsonb_object_keys(schema->'patternProperties') LOOP
        RAISE NOTICE 'prop %s, pattern %, schema %', prop, pattern, schema->'patternProperties'->pattern;
        IF prop ~ pattern AND NOT timetable.validate_json_schema(schema->'patternProperties'->pattern, data->prop, root_schema) THEN
          RETURN false;
        END IF;
      END LOOP;
    END LOOP;
  END IF;

  IF schema ? 'multipleOf' AND jsonb_typeof(data) = 'number' THEN
    IF data::text::numeric % (schema->>'multipleOf')::numeric != 0 THEN
      RETURN false;
    END IF;
  END IF;

  RETURN true;
END;
$$ LANGUAGE 'plpgsql' IMMUTABLE;

