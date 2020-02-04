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

-- is_cron_in_time returns TRUE if timestamp is listed in cron expression
CREATE OR REPLACE FUNCTION timetable.is_cron_in_time(run_at timetable.cron, ts timestamptz) RETURNS BOOLEAN AS
$$
DECLARE 
    a_by_minute integer[];
    a_by_hour integer[];
    a_by_day integer[];
    a_by_month integer[];
    a_by_day_of_week integer[]; 
BEGIN
    IF run_at IS NULL
    THEN
        RETURN TRUE;
    END IF;
    a_by_minute := timetable.cron_element_to_array(run_at, 'minute');
    a_by_hour := timetable.cron_element_to_array(run_at, 'hour');
    a_by_day := timetable.cron_element_to_array(run_at, 'day');
    a_by_month := timetable.cron_element_to_array(run_at, 'month');
    a_by_day_of_week := timetable.cron_element_to_array(run_at, 'day_of_week'); 
    RETURN  (a_by_month[1]       IS NULL OR date_part('month', ts) = ANY(a_by_month))
        AND (a_by_day_of_week[1] IS NULL OR date_part('dow', ts) = ANY(a_by_day_of_week))
        AND (a_by_day[1]         IS NULL OR date_part('day', ts) = ANY(a_by_day))
        AND (a_by_hour[1]        IS NULL OR date_part('hour', ts) = ANY(a_by_hour))
        AND (a_by_minute[1]      IS NULL OR date_part('minute', ts) = ANY(a_by_minute));    
END;
$$ LANGUAGE 'plpgsql';

-- cron_element_to_array() will return array with minutes, hours, days etc. of execution
CREATE OR REPLACE FUNCTION timetable.cron_element_to_array(element text, element_type text) RETURNS integer[] AS
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
                    USING HINT = 'fields seperated by space or tab, Values allowed: numbers (value list with ","), any value with "*", range of value with "-" and step values with "/"!';
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
