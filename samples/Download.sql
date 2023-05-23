-- Prepare the destination table 'location'
CREATE TABLE IF NOT EXISTS city(
    city text,
    lat numeric,
    lng numeric,
    country text,
    iso2 text,
    admin_name text,
    capital text,
    population bigint,
    population_proper bigint);

-- An enhanced example consisting of three tasks:
-- 1. Download text file from internet using BUILT-IN command
-- 2. Remove accents (diacritic signs) from letters using PROGRAM command (can be done with `unaccent` PostgreSQL extension) 
-- 3. Import text file as CSV file using BUILT-IN command (can be down with `psql -c /copy`)
DO $$
DECLARE
    v_head_id bigint;
    v_task_id bigint;
    v_chain_id bigint;
BEGIN
    -- Create the chain with default values executed every minute (NULL == '* * * * *' :: timetable.cron)
    INSERT INTO timetable.chain (chain_name, live)
    VALUES ('Download locations and aggregate', TRUE)
    RETURNING chain_id INTO v_chain_id;

    -- Step 1. Download file from the server
    -- Create the chain
    INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error)
    VALUES (v_chain_id, 1, 'BUILTIN', 'Download', TRUE)
    RETURNING task_id INTO v_task_id;

    -- Create the parameters for the step 1:
    INSERT INTO timetable.parameter (task_id, order_id, value)
        VALUES (v_task_id, 1, 
           '{
                "workersnum": 1,
                "fileurls": ["https://simplemaps.com/static/data/country-cities/mt/mt.csv"], 
                "destpath": "."
            }'::jsonb);
    
    RAISE NOTICE 'Step 1 completed. Chain added with ID: %; DownloadFile task added with ID: %', v_chain_id, v_task_id;

    -- Step 2. Transform Unicode characters into ASCII
    -- Create the program task to call 'uconv' and name it 'unaccent'
    INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error, task_name)
    VALUES (v_chain_id, 2, 'PROGRAM', 'uconv', TRUE, 'unaccent')
    RETURNING task_id INTO v_task_id;

    -- Create the parameters for the 'unaccent' task. Input and output files in this case
    -- Under Windows we should call PowerShell instead of "uconv" with command:
    -- Set-content "orte_ansi.txt" ((Get-content "orte.txt").Normalize("FormD") -replace '\p{M}', '')
    INSERT INTO timetable.parameter (task_id, order_id, value)
        VALUES (v_task_id, 1, '["-x", "Latin-ASCII", "-o", "mt_ansi.csv", "mt.csv"]'::jsonb);

    RAISE NOTICE 'Step 2 completed. Unacent task added with ID: %', v_task_id;

    -- Step 3. Import ASCII file to PostgreSQL table using "CopyFromFile" built-in command
    INSERT INTO timetable.task (chain_id, task_order, kind, command)
        VALUES (v_chain_id, 3, 'BUILTIN', 'CopyFromFile')
    RETURNING task_id INTO v_task_id;

    -- Add the parameters for the download task. Execute client side COPY to 'location' from 'orte_ansi.txt'
    INSERT INTO timetable.parameter (task_id, order_id, value)
        VALUES (v_task_id, 1, '{"sql": "COPY city FROM STDIN (FORMAT csv, HEADER true)", "filename": "mt_ansi.csv" }'::jsonb);

    RAISE NOTICE 'Step 3 completed. Import task added with ID: %', v_task_id;

    INSERT INTO timetable.task (chain_id, task_order, kind, command, ignore_error, task_name)
    VALUES (v_chain_id, 4, 'PROGRAM', 'bash', TRUE, 'remove .csv')
    RETURNING task_id INTO v_task_id;

    INSERT INTO timetable.parameter (task_id, order_id, value)
    VALUES (v_task_id, 1, '["-c", "rm *.csv"]'::jsonb);
END;
$$ LANGUAGE PLPGSQL;