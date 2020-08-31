package pgengine

const sqlJobFunctions = `-- get_running_jobs() returns jobs are running for particular chain_execution_config
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
                a_res := array_append(a_res, allowed_range[1]);
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
    task_name        TEXT,
    task_function    TEXT,
    client_name      TEXT,
    task_type        timetable.task_kind DEFAULT 'SQL'::timetable.task_kind,
    run_at           timetable.cron DEFAULT NULL,
    max_instances    INTEGER DEFAULT NULL,
    live             BOOLEAN DEFAULT false,
    self_destruct    BOOLEAN DEFAULT false
) RETURNS BIGINT AS
'WITH 
    cte_task(v_task_id) AS ( --Create task
        INSERT INTO timetable.base_task 
        VALUES (DEFAULT, task_name, task_type, task_function)
        RETURNING task_id
    ),
    cte_chain(v_chain_id) AS ( --Create chain
        INSERT INTO timetable.task_chain (task_id, ignore_error)
        SELECT v_task_id, TRUE FROM cte_task
        RETURNING chain_id
    )
INSERT INTO timetable.chain_execution_config (
    chain_id, 
    chain_name, 
    run_at, 
    max_instances, 
    live,
    self_destruct,
    client_name
) SELECT 
    v_chain_id, 
    ''chain_'' || v_chain_id, 
    run_at,
    max_instances, 
    live, 
    self_destruct,
    client_name
FROM cte_chain
RETURNING chain_execution_config 
' LANGUAGE 'sql';

CREATE OR REPLACE FUNCTION timetable.next_run(run_at timetable.cron)
 RETURNS timestamp without time zone
AS $$
DECLARE
    a_by_minute integer[];
    a_by_hour integer[];
    a_by_day integer[];
    a_by_month integer[];
    a_by_day_of_week integer[];
    m_minutes integer[];
    m_hours integer[];
    m_days integer[];
    m_months integer[];
    time timestamp;
    dates timestamp[];
    now timestamp := now();
BEGIN
    IF starts_with(run_at :: text, '@') THEN
        RETURN NULL;
    END IF;
    a_by_minute := timetable.cron_element_to_array(run_at, 'minute');
    a_by_hour := timetable.cron_element_to_array(run_at, 'hour');
    a_by_day := timetable.cron_element_to_array(run_at, 'day');
    a_by_month := timetable.cron_element_to_array(run_at, 'month');
    a_by_day_of_week := timetable.cron_element_to_array(run_at, 'day_of_week');

    m_minutes := ARRAY_AGG(minute) from (
        select CASE WHEN minute IS NULL THEN date_part('minute', now + interval '1 minute') ELSE minute END  as minute from (select minute from (select unnest(a_by_minute) as minute) as p1 where minute > date_part('minute', now) or minute is null order by minute limit 1) as p2 union
        select CASE WHEN minute IS NULL THEN 0 ELSE minute END as minute from (select min(minute) as minute from (select unnest(a_by_minute) as minute) as p3) p4) p5;

    m_hours := ARRAY_AGG(hour) from (
        select CASE WHEN hour IS NULL THEN date_part('hour', now) ELSE hour END  as hour from (select hour from (select unnest(a_by_hour) as hour) as p1 where hour = date_part('hour', now) or hour is null order by hour limit 1) as p2 union
        select CASE WHEN hour IS NULL THEN date_part('hour', now + interval '1 hour') ELSE hour END  as hour from (select hour from (select unnest(a_by_hour) as hour) as p1 where hour > date_part('hour', now) or hour is null order by hour limit 1) as p2 union
        select CASE WHEN hour IS NULL THEN 0 ELSE hour END as hour from (select min(hour) as hour from (select unnest(a_by_hour) as hour) as p3) p4) p5;

    m_days := ARRAY_AGG(day) from (
        select CASE WHEN day IS NULL THEN date_part('day', now) ELSE day END  as day from (select day from (select unnest(a_by_day) as day) as p1 where day = date_part('day', now) or day is null order by day limit 1) as p2 union
        select CASE WHEN day IS NULL THEN date_part('day', now + interval '1 day') ELSE day END  as day from (select day from (select unnest(a_by_day) as day) as p1 where day > date_part('day', now) or day is null order by day limit 1) as p2 union
        select CASE WHEN day IS NULL THEN 1 ELSE day END as day from (select min(day) as day from (select unnest(a_by_day) as day) as p3) p4) p5;

    m_months := ARRAY_AGG(month) from (
        select CASE WHEN month IS NULL THEN date_part('month', now) ELSE month END  as month from (select month from (select unnest(a_by_month) as month) as p1 where month = date_part('month', now) or month is null order by month limit 1) as p2 union
        select CASE WHEN month IS NULL THEN date_part('month', now + interval '1 month') ELSE month END  as month from (select month from (select unnest(a_by_month) as month) as p1 where month > date_part('month', now) or month is null order by month limit 1) as p2 union
        select CASE WHEN month IS NULL THEN 1 ELSE month END as month from (select min(month) as month from (select unnest(a_by_month) as month) as p3) p4) p5;

    IF -1 = ANY(a_by_day_of_week) IS NULL THEN
            time := min(date) from (select to_timestamp((year::text || '-' || month::text || '-' || day::text || ' ' || hour::text || ':' || minute::text)::text, 'YYYY-MM-DD HH24:MI') as date from (select  unnest(m_days) as day) as days CROSS JOIN (select unnest(m_months) as month) as months CROSS JOIN (select date_part('year', now) as year union select date_part('year', now + interval '1 year') as year) as years CROSS JOIN (select unnest(m_hours) as hour) as hours CROSS JOIN (select unnest(m_minutes) as minute) as minutes) as dates where date > date_trunc('minute', now);
    ELSE
        dates := array_agg(date) from (select generate_series((date || '-01')::timestamp, ((date || '-01')::timestamp + interval '1 month' - interval '1 day'), '1 day'::interval) date from (select (year::text || '-' || month::text) as date from (select  unnest(m_days) as day) as days CROSS JOIN (select unnest(m_months) as month) as months CROSS JOIN (select date_part('year', now) as year union select date_part('year', now + interval '1 year') as year) as years CROSS JOIN (select unnest(m_hours) as hour) as hours CROSS JOIN (select unnest(m_minutes) as minute) as minutes) as dates) dates where date_part('dow', date) = ANY(a_by_day_of_week) or date_part('day', date) = ANY(m_days);
            time := min(date) from (select (date + (hour || ' hour')::interval + (minute || ' minute')::interval) as date from (select  unnest(dates) as date) as dates CROSS JOIN (select unnest(m_hours) as hour) as hours CROSS JOIN (select unnest(m_minutes) as minute) as minutes) as dates where date > date_trunc('minute', now);
    END IF;

    RETURN time;
END;
$$ LANGUAGE plpgsql;
`
