Getting started
================================================================

A variety of examples can be found in the :doc:`samples`. If you want to migrate from a different scheduler, 
you can use :doc:`migration` scripts.

Add simple job
~~~~~~~~~~~~~~

In a real world usually it's enough to use simple jobs. Under this term we understand:

* job is a chain with only one **task** (step) in it;
* it doesn't use complicated logic, but rather simple **command**;
* it doesn't require complex transaction handling, since one task is implicitely executed as a single transaction.

For such a group of chains we've introduced a special function ``timetable.add_job()``.

.. function:: timetable.add_job(job_name, job_schedule, job_command, ...) RETURNS BIGINT

    Creates a simple one-task chain

    :param job_name: The unique name of the **chain** and **command**.
    :type job_name: text

    :param job_schedule: Time schedule in сron syntax at Postgres server time zone
    :type job_schedule: timetable.cron

    :param job_command: The SQL which will be executed.
    :type job_command: text

    :param job_parameters: Arguments for the chain **command**. Default: ``NULL``.
    :type job_parameters: jsonb    

    :param job_kind: Kind of the command: *SQL*, *PROGRAM* or *BUILTIN*. Default: ``SQL``.
    :type job_kind: timetable.command_kind

    :param job_client_name: Specifies which client should execute the chain. Set this to `NULL` to allow any client. Default: ``NULL``.
    :type job_client_name: text

    :param job_max_instances: The amount of instances that this chain may have running at the same time. Default: ``NULL``.
    :type job_max_instances: integer

    :param job_live: Control if the chain may be executed once it reaches its schedule. Default: ``TRUE``.
    :type job_live: boolean

    :param job_self_destruct: Self destruct the chain after execution. Default: ``FALSE``.
    :type job_self_destruct: boolean

    :param job_ignore_errors: Ignore error during execution. Default: ``TRUE``.
    :type job_ignore_errors: boolean

    :param job_exclusive: Execute the chain in the exclusive mode. Default: ``FALSE``.
    :type job_exclusive: boolean

    :returns: the ID of the created chain
    :rtype: integer

Examples
~~~~~~~~~

#. Run ``public.my_func()`` at 00:05 every day in August Postgres server time zone:

    .. code-block:: SQL

        SELECT timetable.add_job('execute-func', '5 0 * 8 *', 'SELECT public.my_func()');

#. Run `VACUUM` at minute 23 past every 2nd hour from 0 through 20 every day Postgres server time zone:

    .. code-block:: SQL

        SELECT timetable.add_job('run-vacuum', '23 0-20/2 * * *', 'VACUUM');

#. Refresh materialized view every 2 hours:

    .. code-block:: SQL

        SELECT timetable.add_job('refresh-matview', '@every 2 hours', 'REFRESH MATERIALIZED VIEW public.mat_view');

#. Clear log table after **pg_timetable** restart:

    .. code-block:: SQL

        SELECT timetable.add_job('clear-log', '@reboot', 'TRUNCATE timetable.log');

#. Reindex at midnight Postgres server time zone on Sundays with `reindexdb <https://www.postgresql.org/docs/current/app-reindexdb.html>`_ utility:

    - using default database under default user (no command line arguments)
  
        .. code-block:: SQL

            SELECT timetable.add_job('reindex', '0 0 * * 7', 'reindexdb', job_kind := 'PROGRAM');
    
    - specifying target database and tables, and be verbose

        .. code-block:: SQL

            SELECT timetable.add_job('reindex', '0 0 * * 7', 'reindexdb', 
                '["--table=foo", "--dbname=postgres", "--verbose"]'::jsonb, 'PROGRAM');

    - passing password using environment variable through ``bash`` shell

        .. code-block:: SQL

            SELECT timetable.add_job('reindex', '0 0 * * 7', 'bash', 
                '["-c", "PGPASSWORD=5m3R7K4754p4m reindexdb -U postgres -h 192.168.0.221 -v"]'::jsonb, 
                'PROGRAM');                