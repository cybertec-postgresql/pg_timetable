CREATE OR REPLACE FUNCTION bool_array_to_cron(bool[], start_with int4 DEFAULT 0) RETURNS TEXT AS
$$
WITH u AS (
	SELECT unnest($1) e, generate_series($2, array_length($1, 1)-1+$2) AS i 
)
SELECT COALESCE(array_to_string(array_agg(i), ','), '*') FROM u WHERE e
$$
LANGUAGE sql;


WITH
cte_shell(shell, cmd_param) AS (
	VALUES ('sh', '-c') -- set the shell you want to use for batch steps, e.g. "pwsh -c", "cmd /C", "bash -c", "zsh -c"
),
pga_schedule AS (
	SELECT
		s.jscjobid,
		s.jscname,
		format('%s %s %s %s %s', 
			bool_array_to_cron(s.jscminutes), 
			bool_array_to_cron(s.jschours), 
			bool_array_to_cron(s.jscmonthdays), 
			bool_array_to_cron(s.jscmonths, 1), 
			bool_array_to_cron(s.jscweekdays, 1)) AS schedule
	FROM 
		pgagent.pga_schedule s  
			WHERE s.jscenabled 
			AND now() < COALESCE(s.jscend, 'infinity'::timestamptz)
			AND now() > s.jscstart
),
pga_chain AS (
    SELECT
        nextval('timetable.chain_chain_id_seq'::regclass) AS chain_id,
        jobid,
        format('%s @ %s', jobname, jscname) AS jobname,
        jobhostagent,
        jobenabled,
        schedule
    FROM
        pgagent.pga_job JOIN pga_schedule ON jobid = jscjobid
),
cte_chain AS (
	INSERT INTO timetable.chain (chain_id, chain_name, client_name, run_at, live)
	    SELECT 
	    	chain_id, jobname, jobhostagent, schedule, jobenabled
	    FROM
	    	pga_chain
),
pga_step AS (
	SELECT 
		c.chain_id,
		nextval('timetable.task_task_id_seq'::regclass) AS task_id,
		rank() OVER (ORDER BY jstname) AS jstorder,
		jstid,
		jstname,
		jstenabled,
		CASE jstkind WHEN 'b' THEN 'PROGRAM' ELSE 'SQL' END AS jstkind,
		jstcode,
		COALESCE(NULLIF(jstconnstr, ''), CASE WHEN jstdbname > '' THEN 'dbname=' || jstdbname END) AS jstconnstr,
		jstonerror != 'f' AS jstignoreerror
	FROM
		pga_chain c JOIN pgagent.pga_jobstep js ON c.jobid = js.jstjobid
),
cte_tasks AS (
	INSERT INTO timetable.task(task_id, chain_id, task_name, task_order, kind, command, database_connection)
	    SELECT
	    	task_id, chain_id, jstname, jstorder, jstkind::timetable.command_kind, 
	    	CASE jstkind WHEN 'SQL' THEN jstcode ELSE sh.shell END, -- pgagent executes batch steps in the system shell
	    	jstconnstr
	    FROM
	    	pga_step, cte_shell sh
),
cte_parameters AS (
	INSERT INTO timetable.parameter (task_id, order_id, value)
		SELECT 
			task_id, 1, jsonb_build_array(sh.cmd_param, s.jstcode)
	    FROM
	    	pga_step s, cte_shell sh
	    WHERE 
	    	s.jstkind = 'PROGRAM'
)
SELECT * FROM pga_chain;