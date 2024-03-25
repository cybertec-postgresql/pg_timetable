CREATE SCHEMA timetable;

-- define migrations you need to apply
-- every change to the database schema should populate this table.
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
    (0,  '00259 Restart migrations for v4'),
    (1,  '00305 Fix timetable.is_cron_in_time'),
    (2,  '00323 Append timetable.delete_job function'),
    (3,  '00329 Migration required for some new added functions'),
    (4,  '00334 Refactor timetable.task as plain schema without tree-like dependencies'),
    (5,  '00381 Rewrite active chain handling'),
    (6,  '00394 Add started_at column to active_session and active_chain tables'),
    (7,  '00417 Rename LOG database log level to INFO'),
    (8,  '00436 Add txid column to timetable.execution_log'),
    (9,  '00534 Use cron_split_to_arrays() in cron domain check'),
    (10, '00560 Alter txid column to bigint'),
    (11, '00573 Add ability to start a chain with delay'),
    (12, '00575 Add on_error handling'),
    (13, '00629 Add ignore_error column to timetable.execution_log'),
    (14, '00645 Add option to specify time zone per chain');