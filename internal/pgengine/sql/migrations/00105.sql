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