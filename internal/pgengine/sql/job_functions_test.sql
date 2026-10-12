-- Tests for job_functions.sql, run by pgcov (see .github/workflows/build.yml).

-- add_job, add_task
DO $$
DECLARE
    v_chain bigint;
    v_task bigint;
    c timetable.chain;
    t timetable.task;
BEGIN
    v_chain := timetable.add_job('defaults', '* * * * *', 'SELECT 1');
    SELECT * INTO c FROM timetable.chain WHERE chain_id = v_chain;
    ASSERT c.run_at = '* * * * *' AND c.live AND NOT c.self_destruct AND c.client_name IS NULL, c;
    SELECT * INTO t FROM timetable.task WHERE chain_id = v_chain;
    ASSERT t.kind = 'SQL' AND t.command = 'SELECT 1' AND t.ignore_error AND t.autonomous AND t.task_order = 10, t;
    ASSERT (SELECT value FROM timetable.parameter WHERE task_id = t.task_id) IS NULL;

    v_chain := timetable.add_job('full', '@every 1 hour', 'ls',
        job_parameters => '["-l"]', job_kind => 'PROGRAM', job_client_name => 'worker1',
        job_max_instances => 2, job_live => false, job_self_destruct => true,
        job_ignore_errors => false, job_exclusive => true, job_on_error => 'SELECT 0');
    SELECT * INTO c FROM timetable.chain WHERE chain_id = v_chain;
    ASSERT c.max_instances = 2 AND NOT c.live AND c.self_destruct AND c.client_name = 'worker1'
        AND c.exclusive_execution AND c.on_error = 'SELECT 0', c;
    SELECT * INTO t FROM timetable.task WHERE chain_id = v_chain;
    ASSERT t.kind = 'PROGRAM' AND NOT t.ignore_error, t;
    ASSERT (SELECT value FROM timetable.parameter WHERE task_id = t.task_id) = '["-l"]';

    v_task := timetable.add_task('BUILTIN', 'NoOp', t.task_id);
    ASSERT (SELECT task_order FROM timetable.task WHERE task_id = v_task) = 20;
    v_task := timetable.add_task('SQL', 'SELECT 2', t.task_id, -5);
    ASSERT (SELECT task_order FROM timetable.task WHERE task_id = v_task) = 5;
    ASSERT (SELECT count(*) FROM timetable.task WHERE chain_id = v_chain) = 3;

    ASSERT timetable.add_task('SQL', 'orphan', -1) IS NULL, 'unknown parent task';
END;
$$;

-- move_task_up, move_task_down
DO $$
DECLARE
    v_chain bigint;
    t1 bigint; t2 bigint; t3 bigint;
BEGIN
    v_chain := timetable.add_job('move', '* * * * *', 'first');
    SELECT task_id INTO t1 FROM timetable.task WHERE chain_id = v_chain;
    t2 := timetable.add_task('SQL', 'second', t1);
    t3 := timetable.add_task('SQL', 'third', t2);

    ASSERT NOT timetable.move_task_up(t1), 'already first';
    ASSERT NOT timetable.move_task_down(t3), 'already last';
    ASSERT NOT timetable.move_task_up(-1) AND NOT timetable.move_task_down(-1), 'unknown task';

    ASSERT timetable.move_task_up(t2);
    ASSERT ARRAY(SELECT task_id FROM timetable.task WHERE chain_id = v_chain ORDER BY task_order) = ARRAY[t2, t1, t3];
    ASSERT timetable.move_task_down(t2);
    ASSERT ARRAY(SELECT task_id FROM timetable.task WHERE chain_id = v_chain ORDER BY task_order) = ARRAY[t1, t2, t3];
    ASSERT timetable.move_task_down(t1);
    ASSERT ARRAY(SELECT task_id FROM timetable.task WHERE chain_id = v_chain ORDER BY task_order) = ARRAY[t2, t1, t3];
END;
$$;

-- delete_task, delete_job, pause_job, resume_job
DO $$
DECLARE
    v_chain bigint;
    t1 bigint; t2 bigint;
BEGIN
    v_chain := timetable.add_job('lifecycle', '* * * * *', 'first');
    SELECT task_id INTO t1 FROM timetable.task WHERE chain_id = v_chain;
    t2 := timetable.add_task('SQL', 'second', t1);

    ASSERT timetable.delete_task(t2);
    ASSERT NOT timetable.delete_task(t2), 'already deleted';
    ASSERT (SELECT count(*) FROM timetable.task WHERE chain_id = v_chain) = 1;

    ASSERT timetable.pause_job('lifecycle');
    ASSERT NOT (SELECT live FROM timetable.chain WHERE chain_id = v_chain);
    ASSERT timetable.resume_job('lifecycle');
    ASSERT (SELECT live FROM timetable.chain WHERE chain_id = v_chain);
    ASSERT NOT timetable.pause_job('missing') AND NOT timetable.resume_job('missing');

    ASSERT timetable.delete_job('lifecycle');
    ASSERT NOT timetable.delete_job('lifecycle'), 'already deleted';
    ASSERT NOT EXISTS (SELECT 1 FROM timetable.task WHERE chain_id = v_chain), 'tasks cascade';
END;
$$;

-- notify_chain_start, notify_chain_stop: payload shape only, delivery is the client's job
DO $$
BEGIN
    PERFORM timetable.notify_chain_start(1, 'worker');
    PERFORM timetable.notify_chain_start(1, 'worker', INTERVAL '5 seconds');
    PERFORM timetable.notify_chain_stop(1, 'worker');
END;
$$;
