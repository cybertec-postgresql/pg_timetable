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
	excluded_execution_configs	INTEGER[]
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
CREATE TYPE timetable.log_type AS ENUM ('DEBUG', 'NOTICE', 'LOG', 'ERROR', 'PANIC');

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



-----------------------------------------------------------------

-- this stored procedure will tell us which scripts chains
-- have to be executed
-- $1: chain execution config id
CREATE OR REPLACE FUNCTION timetable.check_task(BIGINT) RETURNS BOOLEAN AS
$$
DECLARE	
	v_chain_exec_conf	ALIAS FOR $1;

	v_record		record;
	v_return		BOOLEAN;
BEGIN
	SELECT * 	
		FROM 	timetable.chain_execution_config 
		WHERE 	chain_execution_config = v_chain_exec_conf
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


DROP TRIGGER IF EXISTS trig_task_chain_fixer ON timetable.base_task;

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
			
			SELECT chain_head_id INTO tmp_chain_head_id FROM timetable.task_chain_head
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


-- see which jobs are running
CREATE OR REPLACE FUNCTION timetable.get_running_jobs (BIGINT) RETURNS SETOF record AS $$
	SELECT  chain_execution_config, start_status
		FROM	timetable.run_status
		WHERE 	start_status IN ( SELECT   start_status
				FROM	timetable.run_status
				WHERE	execution_status IN ('STARTED', 'CHAIN_FAILED',
						     'CHAIN_DONE', 'DEAD')
					AND (chain_execution_config = $1 OR chain_execution_config = 0)
				GROUP BY 1
				HAVING count(*) < 2 
				ORDER BY 1)
			AND chain_execution_config = $1 
		GROUP BY 1, 2
		ORDER BY 1, 2 DESC
$$ LANGUAGE 'sql';

CREATE OR REPLACE FUNCTION timetable.insert_base_task(IN task_name TEXT, IN parent_task_id BIGINT DEFAULT NULL)
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
    INSERT INTO timetable.task_chain(parent_id, task_id, ignore_error)
    VALUES (parent_task_id, builtin_id, FALSE)
    RETURNING chain_id INTO result_id;
	RETURN result_id;
END
$$ LANGUAGE 'plpgsql';