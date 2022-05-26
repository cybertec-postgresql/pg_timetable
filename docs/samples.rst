Samples
========

Basic
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
This sample demonstrates how to create a basic one-step chain with parameters. It uses CTE to directly update the
**timetable** schema tables.

.. literalinclude:: ../samples/Basic.sql
    :linenos:
    :language: SQL

Download, Transform and Import
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
This sample demonstrates how to create enhanced three-step chain with parameters. It uses DO statement to directly update the
**timetable** schema tables.

.. literalinclude:: ../samples/Download.sql
    :linenos:
    :language: SQL

Run tasks in autonomous transaction
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
This sample demonstrates how to run special tasks out of chain transaction context. This is useful for special routines and/or 
non-transactional operations, e.g. *CREATE DATABASE*, *REINDEX*, *VACUUM*, *CREATE TABLESPACE*, etc. 

.. literalinclude:: ../samples/Autonomous.sql
    :linenos:
    :language: SQL

Shutdown the scheduler and terminate the session
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
This sample demonstrates how to shutdown the scheduler using special built-in task. This can be used to control 
maintenance windows, to restart the scheduler for update purposes, or to stop session before the database should be 
dropped.

.. literalinclude:: ../samples/Shutdown.sql
    :linenos:
    :language: SQL

Access previous task result code and output from the next task
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
This sample demonstrates how to check the result code and output of a previous task. If previous task failed that possible only
if *ignore_error boolean = true* set for that particular task. Otherwise, the chain will be stopped. This exactly samples
shows how to calculate failed, successful, and total tasks executed. Based on this values we can calculate the success ratio.

.. literalinclude:: ../samples/ManyTasks.sql
    :linenos:
    :language: SQL    