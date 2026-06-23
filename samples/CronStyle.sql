-- Create a job with the timetable.add_job function in cron style

-- In order to demonstrate Cron style schduling of job execution, we will create a table(One time) for inserting of data 
CREATE TABLE IF NOT EXISTS timetable.dummy_log (
    log_ID BIGSERIAL,
    event_name TEXT,
    timestmp TIMESTAMPTZ DEFAULT TRANSACTION_TIMESTAMP(),
    PRIMARY KEY (log_ID));

----CRON-Style
-- * * * * * command to execute
-- ┬ ┬ ┬ ┬ ┬
-- │ │ │ │ │
-- │ │ │ │ └──── day of the week (0 - 7) (Sunday to Saturday)(0 and 7 is Sunday);
-- │ │ │ └────── month (1 - 12)
-- │ │ └──────── day of the month (1 - 31)
-- │ └────────── hour (0 - 23)
-- └──────────── minute (0 - 59)

DELETE FROM timetable.chain WHERE chain_name = 'cron_insert_every_2h_on_27th';

SELECT timetable.add_job (
    job_name     => 'cron_insert_every_2h_on_27th',
    job_schedule => '40 */2 27 * *',
    job_command  => $$INSERT INTO timetable.dummy_log (event_name) VALUES ('Cron test')$$
);