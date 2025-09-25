Migration from others schedulers
================================================

Migrate jobs from pg_cron to pg_timetable
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
If you want to quickly export jobs scheduled from *pg_cron* to *pg_timetable*, you can use this SQL snippet:

.. literalinclude:: ../extras/pg_cron_to_pg_timetable_simple.sql
    :linenos:
    :language: SQL

The *timetable.add_job()*, however, has some limitations. First of all, the function will mark the task created 
as **autonomous**, specifying scheduler should execute the task out of the chain transaction. It's not an error, 
but many autonomous chains may cause some extra connections to be used.

Secondly, database connection parameters are lost for source *pg_cron* jobs, making all jobs local. To export 
every information available precisely as possible, use this SQL snippet under the role they were scheduled in 
*pg_cron*:

.. literalinclude:: ../extras/pg_cron_to_pg_timetable.sql
    :linenos:
    :language: SQL

Migrate jobs from pgAgent to pg_timetable
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
To migrate jobs from **pgAgent**, please use this script. **pgAgent** doesn't have concept of *PROGRAM* task, thus to
emulate *BATCH* steps, **pg_timetable** will execute them inside the shell. You may change the shell by editing *cte_shell*
CTE clause.

.. literalinclude:: ../extras/pgagent_to_pg_timetable.sql
    :linenos:
    :language: SQL