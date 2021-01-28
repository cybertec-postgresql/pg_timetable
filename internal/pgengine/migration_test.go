package pgengine_test

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestMigrations(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()
	pgengine.ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
	pgengine.ConfigDb.Exec(ctx, initialsql)
	ok, err := pgengine.CheckNeedMigrateDb(ctx)
	assert.NoError(t, err)
	assert.True(t, ok, "Should need migrations")
	assert.True(t, pgengine.MigrateDb(ctx), "Migrations should be applied")

}

var initialsql string = `
CREATE SCHEMA timetable;

-- define database connections for script execution
CREATE TABLE timetable.database_connection (
	database_connection BIGSERIAL,
	connect_string 		TEXT		NOT NULL,
	comment 			TEXT,
	PRIMARY KEY (database_connection)
);

-- base tasks: these are the tasks our system actually knows.
-- tasks will be organized in task chains.
--
-- "script" contains either an SQL script, or
--      command string to be executed
--
-- "kind" indicates whether "script" is SQL, built-in function or external program
CREATE TYPE timetable.task_kind AS ENUM ('SQL', 'SHELL', 'BUILTIN');

CREATE TABLE timetable.base_task (
	task_id		BIGSERIAL  			PRIMARY KEY,
	name		TEXT    		    NOT NULL UNIQUE,
	kind		timetable.task_kind	NOT NULL DEFAULT 'SQL',
	script		TEXT				NOT NULL,
	CHECK (CASE WHEN kind <> 'BUILTIN' THEN script IS NOT NULL ELSE TRUE END)
);

-- Task chain declaration:
-- "parent_id" is unique to ensure proper chaining (no trees)
-- "task_id" is the task taken from base_task table
-- "params" is the actual parameters passed to the task
--      upon execution
-- "run_uid" is the username to run as (e.g. su -c "..." - username)
--              (if NULL then don't bother changing UIDs)
-- "ignore_error" indicates whether the next task
--      in the chain can be executed regardless of the
--      success of the current one
CREATE TABLE timetable.task_chain (
	chain_id        	BIGSERIAL	PRIMARY KEY,
	parent_id			BIGINT 		UNIQUE  REFERENCES timetable.task_chain(chain_id)
									ON UPDATE CASCADE
									ON DELETE CASCADE,
	task_id				BIGINT		NOT NULL REFERENCES timetable.base_task(task_id)
									ON UPDATE CASCADE
									ON DELETE CASCADE,
	run_uid				TEXT,
	database_connection	BIGINT		REFERENCES timetable.database_connection(database_connection)
									ON UPDATE CASCADE
									ON DELETE CASCADE,
	ignore_error		BOOLEAN		DEFAULT false
);


-- Task chain execution config. we basically use this table to define when which chain has to
-- be executed.
-- "chain_id" is the first id (parent_id == NULL) of a chain in task_chain
-- "chain_name" is the name of this chain for logging
-- "run_at" is the CRON-style time notation the task has to be run at
-- "max_instances" is the number of instances this chain can run in parallel
-- "live" is the indication that the chain is finalized, the system can run it
-- "self_destruct" is the indication that this chain will delete itself after run
-- "client_name" is the indication that this chain will run only under this tag
CREATE TABLE timetable.chain_execution_config (
    chain_execution_config		BIGSERIAL	PRIMARY KEY,
    chain_id        			BIGINT 		REFERENCES timetable.task_chain(chain_id)
                                            ON UPDATE CASCADE
											ON DELETE CASCADE,
    chain_name      			TEXT		NOT NULL UNIQUE,
    run_at_minute				INTEGER,
    run_at_hour					INTEGER,
    run_at_day					INTEGER,
    run_at_month				INTEGER,
    run_at_day_of_week			INTEGER,
    max_instances				INTEGER,
    live						BOOLEAN		DEFAULT false,
    self_destruct				BOOLEAN		DEFAULT false,
	exclusive_execution			BOOLEAN		DEFAULT false,
	excluded_execution_configs	INTEGER[],
	client_name					TEXT
);


-- parameter passing for config
CREATE TABLE timetable.chain_execution_parameters(
	chain_execution_config	BIGINT	REFERENCES timetable.chain_execution_config (chain_execution_config)
									ON UPDATE CASCADE
									ON DELETE CASCADE,
	chain_id 				BIGINT 	REFERENCES timetable.task_chain(chain_id)
									ON UPDATE CASCADE
									ON DELETE CASCADE,
	order_id 				INTEGER	CHECK (order_id > 0),
	value 					jsonb,
	PRIMARY KEY (chain_execution_config, chain_id, order_id)
);


-- log client application related actions
CREATE TYPE timetable.log_type AS ENUM ('DEBUG', 'NOTICE', 'LOG', 'ERROR', 'PANIC', 'USER');

CREATE TABLE timetable.log
(
	id					BIGSERIAL			PRIMARY KEY,
	ts					TIMESTAMPTZ			DEFAULT now(),
	client_name	        TEXT,
	pid					INTEGER 			NOT NULL,
	log_level			timetable.log_type	NOT NULL,
	message				TEXT
);

-- log timetable related action
CREATE TABLE timetable.execution_log (
	chain_execution_config	BIGINT,
	chain_id        		BIGINT,
	task_id         		BIGINT,
	name            		TEXT		NOT NULL, -- expanded details about the task run
	script          		TEXT,
	kind          			TEXT,
	last_run       	 		TIMESTAMPTZ	DEFAULT now(),
	finished        		TIMESTAMPTZ,
	returncode      		INTEGER,
	pid             		BIGINT
);

CREATE TYPE timetable.execution_status AS ENUM ('STARTED', 'CHAIN_FAILED', 'CHAIN_DONE', 'DEAD');

CREATE TABLE timetable.run_status (
	run_status 					BIGSERIAL,
	start_status				BIGINT,
	execution_status 			timetable.execution_status,
	chain_id 					BIGINT,
	current_execution_element	BIGINT,
	started 					TIMESTAMPTZ,
	last_status_update 			TIMESTAMPTZ 				DEFAULT clock_timestamp(),
	chain_execution_config 		BIGINT,
	PRIMARY KEY (run_status)
);

CREATE OR REPLACE FUNCTION timetable.trig_chain_fixer() RETURNS trigger AS $$
	DECLARE
		tmp_parent_id BIGINT;
		tmp_chain_id BIGINT;
		orig_chain_id BIGINT;
		tmp_chain_head_id BIGINT;
		i BIGINT;
	BEGIN
		--raise notice 'Fixing chain for deletion of base_task#%', OLD.task_id;

		FOR orig_chain_id IN
			SELECT chain_id FROM timetable.task_chain WHERE task_id = OLD.task_id
		LOOP

			--raise notice 'chain_id#%', orig_chain_id;	
			tmp_chain_id := orig_chain_id;
			i := 0;
			LOOP
				i := i + 1;
				SELECT parent_id INTO tmp_parent_id FROM timetable.task_chain
					WHERE chain_id = tmp_chain_id;
				EXIT WHEN tmp_parent_id IS NULL;
				IF i > 100 THEN
					RAISE EXCEPTION 'Infinite loop at timetable.task_chain.chain_id=%', tmp_chain_id;
					RETURN NULL;
				END IF;
				tmp_chain_id := tmp_parent_id;
			END LOOP;
			
			SELECT parent_id INTO tmp_chain_head_id FROM timetable.task_chain
				WHERE chain_id = tmp_chain_id;
				
			--raise notice 'PERFORM task_chain_delete(%,%)', tmp_chain_head_id, orig_chain_id;
			PERFORM timetable.task_chain_delete(tmp_chain_head_id, orig_chain_id);

		END LOOP;
		
		RETURN OLD;
	END;
$$ LANGUAGE 'plpgsql';

CREATE TRIGGER trig_task_chain_fixer
        BEFORE DELETE ON timetable.base_task
        FOR EACH ROW EXECUTE PROCEDURE timetable.trig_chain_fixer();

CREATE OR REPLACE FUNCTION timetable.task_chain_delete(config_ bigint, chain_id_ bigint) RETURNS boolean AS $$
DECLARE
		chain_id_1st_   bigint;
		id_in_chain	 bool;
		chain_id_curs   bigint;
		chain_id_before bigint;
		chain_id_after  bigint;
		curs1 refcursor;
BEGIN
		SELECT chain_id INTO chain_id_1st_ FROM timetable.chain_execution_config WHERE chain_execution_config = config_;
		-- No such chain_execution_config
		IF NOT FOUND THEN
				RAISE NOTICE 'No such chain_execution_config';
				RETURN false;
		END IF;
		-- This head is not connected to a chain
		IF chain_id_1st_ IS NULL THEN
				RAISE NOTICE 'This head is not connected to a chain';
				RETURN false;
		END IF;

		OPEN curs1 FOR WITH RECURSIVE x (chain_id) AS (
				SELECT chain_id FROM timetable.task_chain
				WHERE chain_id = chain_id_1st_ AND parent_id IS NULL
				UNION ALL
				SELECT timetable.task_chain.chain_id FROM timetable.task_chain, x
				WHERE timetable.task_chain.parent_id = x.chain_id
		) SELECT chain_id FROM x;

		id_in_chain = false;
		chain_id_curs = NULL;
		chain_id_before = NULL;
		chain_id_after = NULL;
		LOOP
				FETCH curs1 INTO chain_id_curs;
				IF id_in_chain = false AND chain_id_curs <> chain_id_ THEN
						chain_id_before = chain_id_curs;
				END IF;
				IF chain_id_curs = chain_id_ THEN
						id_in_chain = true;
				END IF;
				EXIT WHEN id_in_chain OR NOT FOUND;
		END LOOP;

		IF id_in_chain THEN
				FETCH curs1 INTO chain_id_after;
		ELSE
				CLOSE curs1;
				RAISE NOTICE 'This chain_id is not part of chain pointed by the chain_execution_config';
				RETURN false;
		END IF;

		CLOSE curs1;

		IF chain_id_before IS NULL THEN
			UPDATE timetable.chain_execution_config SET chain_id = chain_id_after WHERE chain_execution_config = config_;
		END IF;
		UPDATE timetable.task_chain SET parent_id = NULL WHERE chain_id = chain_id_;
		UPDATE timetable.task_chain SET parent_id = chain_id_before WHERE chain_id = chain_id_after;
		DELETE FROM timetable.task_chain WHERE chain_id = chain_id_;

		RETURN true;
END
$$ LANGUAGE plpgsql;

-- get_running_jobs() returns jobs are running for particular chain_execution_config
CREATE OR REPLACE FUNCTION timetable.get_running_jobs(BIGINT) 
RETURNS SETOF record AS $$
    SELECT  chain_execution_config, start_status
        FROM    timetable.run_status
        WHERE   start_status IN ( SELECT   start_status
                FROM    timetable.run_status
                WHERE   execution_status IN ('STARTED', 'CHAIN_FAILED',
                             'CHAIN_DONE', 'DEAD')
                    AND (chain_execution_config = $1 OR chain_execution_config = 0)
                GROUP BY 1
                HAVING count(*) < 2 
                ORDER BY 1)
            AND chain_execution_config = $1 
        GROUP BY 1, 2
        ORDER BY 1, 2 DESC
$$ LANGUAGE 'sql';

CREATE OR REPLACE FUNCTION timetable.insert_base_task(IN task_name TEXT, IN parent_task_id BIGINT)
RETURNS BIGINT AS $$
DECLARE
    builtin_id BIGINT;
    result_id BIGINT;
BEGIN
    SELECT task_id FROM timetable.base_task WHERE name = task_name INTO builtin_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Nonexistent builtin task --> %', task_name
        USING 
            ERRCODE = 'invalid_parameter_value',
            HINT = 'Please check your user task name parameter';
    END IF;
    INSERT INTO timetable.task_chain 
        (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
    VALUES 
        (DEFAULT, parent_task_id, builtin_id, NULL, NULL, FALSE)
    RETURNING chain_id INTO result_id;
    RETURN result_id;
END
$$ LANGUAGE 'plpgsql';

-- check_task() stored procedure will tell us if chain execution config id have to be executed 
CREATE OR REPLACE FUNCTION timetable.check_task(BIGINT) RETURNS BOOLEAN AS
$$
DECLARE 
    v_chain_exec_conf   ALIAS FOR $1;
    v_record        record;
    v_return        BOOLEAN;
BEGIN
    SELECT *    
        FROM    timetable.chain_execution_config 
        WHERE   chain_execution_config = v_chain_exec_conf
        INTO v_record;

    IF NOT FOUND
    THEN
        RETURN FALSE;
    END IF;
    
    -- ALL NULLS means task executed every minute
    RETURN  COALESCE(v_record.run_at_month, v_record.run_at_day_of_week, v_record.run_at_day,
            v_record.run_at_hour,v_record.run_at_minute) IS NULL
        OR 
            COALESCE(v_record.run_at_month = date_part('month', now()), TRUE)
        AND COALESCE(v_record.run_at_day_of_week = date_part('dow', now()), TRUE)
        AND COALESCE(v_record.run_at_day = date_part('day', now()), TRUE)
        AND COALESCE(v_record.run_at_hour = date_part('hour', now()), TRUE)
        AND COALESCE(v_record.run_at_minute = date_part('minute', now()), TRUE);
END;
$$ LANGUAGE 'plpgsql';

-- cron_element_to_array() will return array with minutes, hours, days etc. of execution
CREATE OR REPLACE FUNCTION timetable.cron_element_to_array(
    element text,
    element_type text)
    RETURNS integer[] AS
$$
DECLARE
    a_element text[];
    i_index integer;
    a_tmp text[] := '{}';
    tmp_item text;
    a_range text[];
    a_split text[];
    counter integer;
    counter_range integer[];
    a_res integer[] := '{}';
    allowed_range integer[];
    max_val integer;
    min_val integer;
BEGIN
    IF lower(element_type) = 'minute' THEN
        i_index = 1;
        allowed_range = '{0,59}';
    ELSIF lower(element_type) = 'hour' THEN
        i_index = 2;
        allowed_range = '{0,23}';
    ELSIF lower(element_type) = 'day' THEN
        i_index = 3;
        allowed_range = '{1,31}';
    ELSIF lower(element_type) = 'month' THEN
        i_index = 4;
        allowed_range = '{1,12}';
    ELSIF lower(element_type) = 'day_of_week' THEN
        i_index = 5;
        allowed_range = '{0,7}';
    ELSE
        RAISE EXCEPTION 'element_type ("%") not recognized', element_type
            USING HINT = 'Allowed values are "minute, day, hour, month, day_of_month"!';
    END IF;


    a_element := regexp_split_to_array(element, '\s+');
    a_tmp := string_to_array(a_element[i_index],',');

    FOREACH  tmp_item IN ARRAY a_tmp
        LOOP
            -- normal integer
            IF tmp_item ~ '^[0-9]+$' THEN
                a_res := array_append(a_res, tmp_item::int);

                -- '*' any value
            ELSIF tmp_item ~ '^[*]+$' THEN
                a_res := array_append(a_res, NULL);

                -- '-' range of values
            ELSIF tmp_item ~ '^[0-9]+[-][0-9]+$' THEN
                a_range := regexp_split_to_array(tmp_item, '-');
                a_range := array(select generate_series(a_range[1]::int,a_range[2]::int));
                a_res := array_cat(a_res, a_range::int[]);

                -- '/' step values
            ELSIF tmp_item ~ '^[0-9]+[\/][0-9]+$' THEN
                a_split := regexp_split_to_array(tmp_item, '/');
                counter := a_split[1]::int;
                WHILE counter+a_split[2]::int <= 59 LOOP
                    a_res := array_append(a_res, counter);
                    counter := counter + a_split[2]::int ;
                END LOOP ;

                --Heavy sh*t, combinated special chars
                -- '-' range of values and '/' step values
            ELSIF tmp_item ~ '^[0-9-]+[\/][0-9]+$' THEN
                a_split := regexp_split_to_array(tmp_item, '/');
                counter_range := regexp_split_to_array(a_split[1], '-');
                WHILE counter_range[1]::int+a_split[2]::int <= counter_range[2]::int LOOP
                    a_res := array_append(a_res, counter_range[1]);
                    counter_range[1] := counter_range[1] + a_split[2]::int ;
                END LOOP;

                -- '*' any value and '/' step values
            ELSIF tmp_item ~ '^[*]+[\/][0-9]+$' THEN
                a_split := regexp_split_to_array(tmp_item, '/');
                counter_range := allowed_range;
                WHILE counter_range[1]::int+a_split[2]::int <= counter_range[2]::int LOOP
                    counter_range[1] := counter_range[1] + a_split[2]::int ;
                    a_res := array_append(a_res, counter_range[1]);
                END LOOP;
            ELSE
                RAISE EXCEPTION 'Value ("%") not recognized', a_element[i_index]
                    USING HINT = 'fields separated by space or tab, Values allowed: numbers (value list with ","), any value with "*", range of value with "-" and step values with "/"!';
            END IF;
        END LOOP;

    --sort the array ;)
    SELECT ARRAY_AGG(x.val) INTO a_res
    FROM (SELECT UNNEST(a_res) AS val ORDER BY val) AS x;

    --check if Values in allowed ranges
    max_val :=  max(x) FROM unnest(a_res) as x;
    min_val :=  min(x) FROM unnest(a_res) as x;
    IF max_val > allowed_range[2] OR min_val < allowed_range[1] then
        RAISE EXCEPTION '%s incorrect, allowed range between % and %', INITCAP(element_type), allowed_range[1], allowed_range[2]  ;
    END IF;

    RETURN a_res;
END;
$$ LANGUAGE 'plpgsql';

-- job_add() will add job to the system
CREATE OR REPLACE FUNCTION timetable.job_add(
    task_name text,
    task_function text,
    client_name text,
    task_type timetable.task_kind DEFAULT 'SQL'::timetable.task_kind,
    by_cron text DEFAULT NULL::text,
    by_minute text DEFAULT NULL::text,
    by_hour text DEFAULT NULL::text,
    by_day text DEFAULT NULL::text,
    by_month text DEFAULT NULL::text,
    by_day_of_week text DEFAULT NULL::text,
    max_instances integer DEFAULT NULL::integer,
    live boolean DEFAULT false,
    self_destruct boolean DEFAULT false)
    RETURNS text AS
$$
DECLARE
    v_task_id bigint;
    v_chain_id bigint;
    v_chain_name text;
    c_matrix refcursor;
    r_matrix record;
    a_by_cron text[];
    a_by_minute integer[];
    a_by_hour integer[];
    a_by_day integer[];
    a_by_month integer[];
    a_by_day_of_week integer[];
    tmp_num numeric;
BEGIN
    --Create task
    INSERT INTO timetable.base_task 
    VALUES (DEFAULT, task_name, task_type, task_function)
    RETURNING task_id INTO v_task_id;

    --Create chain
    INSERT INTO timetable.task_chain (chain_id, parent_id, task_id, run_uid, database_connection, ignore_error)
    VALUES (DEFAULT, NULL, v_task_id, NULL, NULL, TRUE)
    RETURNING chain_id INTO v_chain_id;

    IF by_cron IS NOT NULL then
        a_by_minute	:= timetable.cron_element_to_array(by_cron, 'minute');
        a_by_hour := timetable.cron_element_to_array(by_cron, 'hour');
        a_by_day := timetable.cron_element_to_array(by_cron, 'day');
        a_by_month := timetable.cron_element_to_array(by_cron, 'month');
        a_by_day_of_week := timetable.cron_element_to_array(by_cron, 'day_of_week');
    ELSE
        a_by_minute := string_to_array(by_minute, ',');
        a_by_hour := string_to_array(by_hour, ',');
        IF lower(by_day) = 'weekend' then
            a_by_day := '{6,0}'; -- Saturday,Sunday
        ELSEIF lower(by_day) = 'workweek' then
            a_by_day := '{1,2,3,4,5}'; 	-- Monday-Friday
        ELSEIF lower(by_day) = 'daily' then
            a_by_day := '{0,1,2,3,4,5,6,}';	-- Monday-Sunday
        ELSE
            a_by_day := string_to_array(by_day, ',');
        END IF;
        a_by_month := string_to_array(by_month, ',');
        a_by_day_of_week := string_to_array(by_day_of_week, ',');
    END IF;

    IF a_by_minute IS NOT NULL then
        FOREACH  tmp_num IN ARRAY a_by_minute
            LOOP
                IF tmp_num > 59 OR tmp_num < 0 then
                    RAISE EXCEPTION 'Minutes incorrect'
                        USING HINT = 'Dude Minutes are between 0 and 59 not more or less ;)';
                END IF;
            END LOOP;
    END IF;

    IF a_by_hour IS NOT NULL then
        FOREACH  tmp_num IN ARRAY a_by_hour
            LOOP
                IF tmp_num > 23 OR tmp_num < 0 then
                    RAISE EXCEPTION 'Hours incorrect'
                        USING HINT = 'Dude Hours are between 0 and 23 not more or less ;)';
                END IF;
            END LOOP;
    END IF;

    IF a_by_day IS NOT NULL then
        FOREACH  tmp_num IN ARRAY a_by_day
            LOOP
                IF tmp_num > 31 OR tmp_num < 1 then
                    RAISE EXCEPTION 'Days incorrect'
                        USING HINT = 'Dude Days are between 1 and 31 not more or less ;)';
                END IF;
            END LOOP;
    END IF;

    IF a_by_month IS NOT NULL then
        FOREACH  tmp_num IN ARRAY a_by_month
            LOOP
                IF tmp_num > 12 OR tmp_num < 1 then
                    RAISE EXCEPTION 'Months incorrect'
                        USING HINT = 'Dude Months are between 1 and 12 not more or less ;)';
                END IF;
            END LOOP;
    END IF;

    IF a_by_day_of_week IS NOT NULL then
        FOREACH  tmp_num IN ARRAY a_by_day_of_week
            LOOP
                IF tmp_num > 7 OR tmp_num < 0 then
                    RAISE EXCEPTION 'Days of week incorrect'
                        USING HINT = 'Dude Days of week are between 0 and 7 (0 and 7 are Sunday)';
                END IF;
            END LOOP;
    END IF;

    OPEN c_matrix FOR 
      SELECT *
      FROM
          unnest(a_by_minute) v_min(min) CROSS JOIN 
          unnest(a_by_hour) v_hour(hour) CROSS JOIN 
          unnest(a_by_day) v_day(day) CROSS JOIN 
          unnest(a_by_month) v_month(month) CROSS JOIN 
          unnest(a_by_day_of_week) v_day_of_week(dow)
      ORDER BY
          min, hour, day, month, dow;

    LOOP
        FETCH c_matrix INTO r_matrix;
        EXIT WHEN NOT FOUND;
        RAISE NOTICE 'min: %, hour: %, day: %, month: %',r_matrix.min, r_matrix.hour, r_matrix.day, r_matrix.month;

        v_chain_name := 'chain_'||v_chain_id||'_'||LPAD(COALESCE(r_matrix.min, -1)::text, 2, '0')||LPAD(COALESCE(r_matrix.hour, -1)::text, 2, '0')||LPAD(COALESCE(r_matrix.day, -1)::text, 2, '0')||LPAD(COALESCE (r_matrix.month, -1)::text, 2, '0')||LPAD(COALESCE(r_matrix.dow, -1)::text, 2, '0');
        RAISE NOTICE 'chain_name: %',v_chain_name;


        INSERT INTO timetable.chain_execution_config VALUES
        (
           DEFAULT, -- chain_execution_config,
           v_chain_id, -- chain_id,
           v_chain_name, -- chain_name,
           r_matrix.min, -- run_at_minute,
           r_matrix.hour, -- run_at_hour,
           r_matrix.day, -- run_at_day,
           r_matrix.month, -- run_at_month,
           r_matrix.dow, -- run_at_day_of_week,
           max_instances, -- max_instances,
           live, -- live,
           self_destruct, -- self_destruct,
           FALSE, -- exclusive_execution,
           NULL -- excluded_execution_configs
        );
    END LOOP;
    CLOSE c_matrix;

    RETURN format('JOB_ID: %s, is Created, EXCEUTE TIMES: Minutes: %s, Hours: %s, Days: %s, Months: %s, Day of Week: %s'
        ,v_task_id, a_by_minute, a_by_hour, a_by_day, a_by_month, a_by_day_of_week);

END;
$$ LANGUAGE 'plpgsql';

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

INSERT INTO timetable.base_task(task_id, name, script, kind) VALUES
	(DEFAULT, 'NoOp', 'NoOp', 'BUILTIN'),
	(DEFAULT, 'Sleep', 'Sleep', 'BUILTIN'),
	(DEFAULT, 'Log', 'Log', 'BUILTIN'),
	(DEFAULT, 'SendMail', 'SendMail', 'BUILTIN'),
	(DEFAULT, 'Download', 'Download', 'BUILTIN');

CREATE OR REPLACE FUNCTION timetable.get_task_id(task_name TEXT) 
RETURNS BIGINT AS $$
	SELECT task_id FROM timetable.base_task WHERE name = $1;
$$ LANGUAGE 'sql'
STRICT;`
