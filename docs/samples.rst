Samples
========

Basic
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
This sample demonstrates how to create a basic one-step chain with parameters. It uses CTE to directly update the
**timetable** schema tables.

.. literalinclude:: ../samples/basic.sql
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