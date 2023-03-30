Introduction
================================================

**pg_timetable** is an advanced job scheduler for PostgreSQL, offering many advantages over traditional schedulers such as **cron** and others.
It is completely database driven and provides a couple of advanced concepts.

Main features
--------------

- Tasks can be arranged in chains
- A chain can consist of built-int commands, SQL and executables
- Parameters can be passed to chains
- Missed tasks (possibly due to downtime) can be retried automatically
- Support for configurable repetitions
- Built-in tasks such as sending emails, etc.
- Fully database driven configuration
- Full support for database driven logging
- Cron-style scheduling at the PostgreSQL server time zone
- Optional concurrency protection
- Task and chain can have execution timeout settings

Quick Start
------------

1. Download pg_timetable executable
2. Make sure your PostgreSQL server is up and running and has a role with ``CREATE`` privilege 
   for a target database, e.g.

    .. code-block:: SQL

      my_database=> CREATE ROLE scheduler PASSWORD 'somestrong' WITH LOGIN;
      my_database=> GRANT CREATE ON DATABASE my_database TO scheduler;

3. Run the **pg_timetable** to create **timetable** schema in your database

    .. code-block::

      # pg_timetable postgresql://scheduler:somestrong@localhost/my_database --clientname=vacuumer


4. Create a new job, e.g. run ``VACUUM`` each night at 00:30 Postgres server time zone

    .. code-block:: SQL

      my_database=> SELECT timetable.add_job('frequent-vacuum', '30 * * * *', 'VACUUM');
      add_job
      ---------
            3
      (1 row)

5. PROFIT!

Command line options
------------------------
.. code-block::

  # ./pg_timetable

  Application Options:
    -c, --clientname=                                Unique name for application instance [$PGTT_CLIENTNAME]
        --config=                                    YAML configuration file
        --no-program-tasks                           Disable executing of PROGRAM tasks [$PGTT_NOPROGRAMTASKS]
    -v, --version                                    Output detailed version information [$PGTT_VERSION]
  
  Connection:
    -h, --host=                                      PostgreSQL host (default: localhost) [$PGTT_PGHOST]
    -p, --port=                                      PostgreSQL port (default: 5432) [$PGTT_PGPORT]
    -d, --dbname=                                    PostgreSQL database name (default: timetable) [$PGTT_PGDATABASE]
    -u, --user=                                      PostgreSQL user (default: scheduler) [$PGTT_PGUSER]
        --password=                                  PostgreSQL user password [$PGTT_PGPASSWORD]
        --sslmode=[disable|require]                  Connection SSL mode (default: disable) [$PGTT_PGSSLMODE]
        --pgurl=                                     PostgreSQL connection URL [$PGTT_URL]
        --timeout=                                   PostgreSQL connection timeout (default: 90) [$PGTT_TIMEOUT]
  
  Logging:
        --log-level=[debug|info|error]               Verbosity level for stdout and log file (default: info)
        --log-database-level=[debug|info|error|none] Verbosity level for database storing (default: info)
        --log-file=                                  File name to store logs
        --log-file-format=[json|text]                Format of file logs (default: json)
        --log-file-rotate                            Rotate log files
        --log-file-size=                             Maximum size in MB of the log file before it gets rotated (default: 100)
        --log-file-age=                              Number of days to retain old log files, 0 means forever (default: 0)
        --log-file-number=                           Maximum number of old log files to retain, 0 to retain all (default: 0)
  
  Start:
    -f, --file=                                      SQL script file to execute during startup
        --init                                       Initialize database schema to the latest version and exit. Can be used
                                                     with --upgrade
        --upgrade                                    Upgrade database to the latest version
        --debug                                      Run in debug mode. Only asynchronous chains will be executed
  
  Resource:
        --cron-workers=                              Number of parallel workers for scheduled chains (default: 16)
        --interval-workers=                          Number of parallel workers for interval chains (default: 16)
        --chain-timeout=                             Abort any chain that takes more than the specified number of
                                                     milliseconds
        --task-timeout=                              Abort any task within a chain that takes more than the specified number
                                                     of milliseconds
  
  REST:
        --rest-port=                                 REST API port (default: 0) [$PGTT_RESTPORT]


Contributing
------------
If you want to contribute to **pg_timetable** and help make it better, feel free to open an 
`issue <https://github.com/cybertec-postgresql/pg_timetable/issues>`_ or even consider submitting a 
`pull request <https://github.com/cybertec-postgresql/pg_timetable/pulls>`_. You also can give a 
`star <https://github.com/cybertec-postgresql/pg_timetable/stargazers>`_ to **pg_timetable** project, 
and to tell the world about it.

Support
------------
For professional support, please contact `Cybertec <https://www.cybertec-postgresql.com/>`_.


Authors
---------
Implementation:                `Pavlo Golub <https://github.com/pashagolub>`_ 

Initial idea and draft design: `Hans-Jürgen Schönig <https://github.com/postgresql007>`_
