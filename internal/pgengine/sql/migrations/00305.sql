CREATE OR REPLACE FUNCTION timetable.is_cron_in_time(
    run_at timetable.cron, 
    ts timestamptz
) RETURNS BOOLEAN AS $$
    SELECT
    CASE WHEN run_at IS NULL THEN
        TRUE
    ELSE
        date_part('month', ts) = ANY(a.months)
        AND (date_part('dow', ts) = ANY(a.dow) OR date_part('isodow', ts) = ANY(a.dow))
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