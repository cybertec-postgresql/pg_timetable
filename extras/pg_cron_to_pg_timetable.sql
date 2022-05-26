SET ROLE 'scheduler'; -- set the role used by pg_cron

WITH cron_chain AS (
    SELECT
        nextval('timetable.chain_chain_id_seq'::regclass) AS cron_id,
        jobname,
        schedule,
        active,
        command,
        CASE WHEN 
            database != current_database()
            OR nodename != 'localhost'
            OR username != CURRENT_USER
            OR nodeport != inet_server_port() 
        THEN
            format('host=%s port=%s dbname=%s user=%s', nodename, nodeport, database, username)
        END AS connstr
    FROM
        cron.job
),
cte_chain AS (
	INSERT INTO timetable.chain (chain_id, chain_name, run_at, live)
	    SELECT 
	    	cron_id, COALESCE(jobname, 'cronjob' || cron_id), schedule, active
	    FROM
	    	cron_chain
),
cte_tasks AS (
	INSERT INTO timetable.task (chain_id, task_order, kind, command, database_connection)
	    SELECT
	    	cron_id, 1, 'SQL', command, connstr
	    FROM
	    	cron_chain
	    RETURNING
	        chain_id, task_id
)
SELECT * FROM cte_tasks;