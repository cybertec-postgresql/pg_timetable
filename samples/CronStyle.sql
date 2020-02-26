-- Create a Job with the timetable.job_add function in cron style

-- In order to demonstrate Cron style schduling of job execution, we will create a table(One time) for inserting of data 
CREATE TABLE IF NOT EXISTS timetable.dummy_log (
    log_ID BIGSERIAL,
    event_name TEXT,
    timestmp TIMESTAMPTZ DEFAULT TRANSACTION_TIMESTAMP(),
    PRIMARY KEY (log_ID));

-- Paramerters detail for timetable.job_add()
-- task_name: The name of the Task
-- task_function: The function wich will be executed.
-- client_name: The name of worker under which this task will execute
-- task_type: Type of the function SQL,SHELL and BUILTIN
-- run_at: Time Schedule in Cron Syntax
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

SELECT timetable.job_add (
    task_name      => 'cron_Job run after 40th minutes after 2 hour on 27th of every month ',
    task_function  => $$INSERT INTO timetable.dummy_log (event_name) VALUES ('Cron test')$$,
    client_name    => NULL, -- any worker may execute
    task_type      => 'SQL',
    run_at         => '40 */2 27 * *',
    max_instances  => 1,
    live           => TRUE,
    self_destruct  => FALSE);