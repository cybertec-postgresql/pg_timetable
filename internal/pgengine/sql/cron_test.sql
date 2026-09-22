-- Tests for cron.sql, run by pgcov (see .github/workflows/build.yml).
SET timezone TO 'UTC';

CREATE FUNCTION pg_temp.raises(stmt text, expected_state text) RETURNS text AS $$
BEGIN
    EXECUTE stmt;
    RAISE EXCEPTION 'expected SQLSTATE % from: %', expected_state, stmt;
EXCEPTION WHEN OTHERS THEN
    IF SQLSTATE <> expected_state THEN
        RAISE EXCEPTION 'expected SQLSTATE % but got %: %', expected_state, SQLSTATE, SQLERRM;
    END IF;
    RETURN SQLERRM;
END
$$ LANGUAGE plpgsql;

-- cron_split_to_arrays
DO $$
DECLARE
    r record;
BEGIN
    r := timetable.cron_split_to_arrays('0 12 * * *');
    ASSERT r.mins = ARRAY[0], r.mins;
    ASSERT r.hours = ARRAY[12], r.hours;
    ASSERT array_length(r.days, 1) = 31, r.days;
    ASSERT array_length(r.months, 1) = 12, r.months;
    ASSERT r.dow = ARRAY[0,1,2,3,4,5,6,7], r.dow;

    r := timetable.cron_split_to_arrays('3,1,1-2 */6 5/10 1-10/3 1');
    ASSERT r.mins = ARRAY[1,2,3], r.mins;            -- list + range, deduplicated and sorted
    ASSERT r.hours = ARRAY[0,6,12,18], r.hours;      -- */step
    ASSERT r.days = ARRAY[5,15,25], r.days;          -- start/step
    ASSERT r.months = ARRAY[1,4,7,10], r.months;     -- range/step
    ASSERT r.dow = ARRAY[1], r.dow;

    ASSERT timetable.cron_split_to_arrays(NULL) IS NULL;

    ASSERT pg_temp.raises($q$ SELECT timetable.cron_split_to_arrays('60 * * * *') $q$, 'P0001')
        LIKE '60 is out of range%minutes', 'out of range (minutes)';
    ASSERT pg_temp.raises($q$ SELECT timetable.cron_split_to_arrays('* * 0 * *') $q$, 'P0001')
        LIKE '0 is out of range%days', 'out of range (days)';
    ASSERT pg_temp.raises($q$ SELECT timetable.cron_split_to_arrays('foo * * * *') $q$, 'P0001')
        LIKE 'Value ("foo") not recognized', 'unrecognized value';
END;
$$;

-- cron_months, cron_days, cron_times
DO $$
BEGIN
    ASSERT ARRAY(SELECT timetable.cron_months('2024-01-15', ARRAY[3])) = ARRAY['2024-03-01'::timestamptz];
    -- a one-year window includes the same month twice
    ASSERT (SELECT count(*) FROM timetable.cron_months('2024-01-15', ARRAY[1])) = 2;

    ASSERT ARRAY(SELECT timetable.cron_days('2024-01-15', ARRAY[2], ARRAY[29], ARRAY[0,1,2,3,4,5,6,7]))
        = ARRAY['2024-02-29'::timestamptz], 'leap day';
    ASSERT (SELECT count(*) FROM timetable.cron_days('2024-01-15', ARRAY[2], ARRAY(SELECT generate_series(1,31)), ARRAY[1])) = 4,
        'Mondays in February 2024';

    ASSERT (SELECT count(*) FROM timetable.cron_times(ARRAY[1,2], ARRAY[0,30])) = 4;
    ASSERT '01:30'::time IN (SELECT timetable.cron_times(ARRAY[1,2], ARRAY[0,30]));
END;
$$;

-- cron_runs, next_run
DO $$
BEGIN
    ASSERT (SELECT min(r) FROM timetable.cron_runs('2024-01-15 10:00+00', '0 12 * * *') r) = '2024-01-15 12:00+00';
    -- runs must be strictly after from_ts
    ASSERT (SELECT min(r) FROM timetable.cron_runs('2024-01-15 12:00+00', '0 12 * * *') r) = '2024-01-16 12:00+00';
    ASSERT (SELECT count(*) FROM timetable.cron_runs('2024-01-15 10:00+00', '0 12 1 1 *')) = 1;

    ASSERT timetable.next_run('* * * * *') BETWEEN now() AND now() + INTERVAL '1 minute';
    ASSERT timetable.next_run(NULL) IS NULL;
END;
$$;

-- is_cron_in_time
DO $$
BEGIN
    ASSERT timetable.is_cron_in_time(NULL, now());
    ASSERT timetable.is_cron_in_time('0 12 * * *', '2024-01-15 12:00+00');
    ASSERT NOT timetable.is_cron_in_time('0 12 * * *', '2024-01-15 13:00+00');
    ASSERT NOT timetable.is_cron_in_time('0 12 * 2 *', '2024-01-15 12:00+00');
    ASSERT timetable.is_cron_in_time('* * * * 1', '2024-01-15 12:00+00'), 'Monday';
    ASSERT timetable.is_cron_in_time('* * * * 0', '2024-01-14 12:00+00'), 'Sunday as 0';
    ASSERT timetable.is_cron_in_time('* * * * 7', '2024-01-14 12:00+00'), 'Sunday as 7';
    ASSERT NOT timetable.is_cron_in_time('* * * * 1', '2024-01-14 12:00+00');
END;
$$;

-- timetable.cron domain
DO $$
BEGIN
    PERFORM '@reboot'::timetable.cron;
    PERFORM '@every 1 hour'::timetable.cron;
    PERFORM '@after 5 minutes'::timetable.cron;
    PERFORM '*/5 0-6 1,15 * 1-5'::timetable.cron;

    PERFORM pg_temp.raises($q$ SELECT 'garbage'::timetable.cron $q$, '23514');
    PERFORM pg_temp.raises($q$ SELECT '* * * *'::timetable.cron $q$, '23514');
    PERFORM pg_temp.raises($q$ SELECT '@every soon'::timetable.cron $q$, '22007');
    PERFORM pg_temp.raises($q$ SELECT '60 * * * *'::timetable.cron $q$, 'P0001');
END;
$$;
