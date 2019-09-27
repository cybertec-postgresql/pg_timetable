
-- Create a Job with the timetable.job_add function in cron style

-- In order to demonstrate Cron style schduling of job execution, we will create a table(One time) for inserting of data 
CREATE TABLE timetable.dummy_log (
    log_ID BIGSERIAL,
    event_name TEXT,
    timestmp TIMESTAMPTZ,
    PRIMARY KEY (log_ID));



CREATE OR REPLACE FUNCTION timetable.insert_dummy_log () RETURNS VOID AS
$$
BEGIN
    INSERT INTO timetable.dummy_log (event_name, timestmp)
    VALUES ('Cron test', TRANSACTION_TIMESTAMP());
END
$$ 
LANGUAGE 'plpgsql';

-- Paramerters detail for timetable.job_add()
-- task_name: The name of the Task
-- task_function: The function wich will be executed.
-- task_type: Type of the function SQL,SHELL and BUILTIN
-- by_cron: Time Schedule in Cron Syntax
-- by_minute: This specifies the minutes on which the job is to run
-- by_hour: This specifies the hours on which the job is to run
-- by_day: This specifies the days on which the job is to run.
-- by_month: This specifies the month on which the job is to run
-- by_day_of_week: This specifies the day of week (0,7 is sunday) on which the job is to run
-- max_instances: The amount of instances that this chain may have running at the same time.
-- live: Control if the chain may be executed once it reaches its schedule.
-- self_destruct: Self destruct the chain.

----CRON-Style
-- * * * * * command to execute
-- ┬ ┬ ┬ ┬ ┬
-- │ │ │ │ │
-- │ │ │ │ └──── day of the week (0 - 7) (Sunday to Saturday)(0 and 7 is Sunday);
-- │ │ │ └────── month (1 - 12)
-- │ │ └──────── day of the month (1 - 31)
-- │ └────────── hour (0 - 23)
-- └──────────── minute (0 - 59)

SELECT
timetable.job_add ('cron_Job run after 40th minutes after 2 hour on 27th of every month ',
    'SELECT timetable.insert_dummy_log()',
    'SQL',
    '40 */2 27 * *',
    '',
    '',
    '',
    '',
    '',
    1,
    TRUE,
    FALSE);
