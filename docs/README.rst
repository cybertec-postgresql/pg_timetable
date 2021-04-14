Introduction
================================================

**pg_timetable** is an advanced job scheduler for PostgreSQL, offering many advantages over traditional schedulers such as **cron** and others.
It is completely database driven and provides a couple of advanced concepts.

Main features
-------------

- Tasks can be arranged in chains
- A chain can consist of built-int commands, SQL and executables
- Parameters can be passed to chains
- Missed tasks (possibly due to downtime) can be retried automatically
- Support for configurable repetitions
- Built-in tasks such as sending emails, etc.
- Fully database driven configuration
- Full support for database driven logging
- Cron-style scheduling
- Optional concurrency protection

Quick Start
-----------

1. Download pg_timetable executable
2. Make sure your PostgreSQL server is up and running and has a role with `CREATE` privilege

::

  # ./pg_timetable

  Application Options:
    -c, --clientname=               Unique name for application instance
    -v, --verbose                   Show verbose debug information [$PGTT_VERBOSE]
    -h, --host=                     PG config DB host (default: localhost) [$PGTT_PGHOST]
    -p, --port=                     PG config DB port (default: 5432) [$PGTT_PGPORT]
    -d, --dbname=                   PG config DB dbname (default: timetable) [$PGTT_PGDATABASE]
    -u, --user=                     PG config DB user (default: scheduler) [$PGTT_PGUSER]
    -f, --file=                     SQL script file to execute during startup
        --password=                 PG config DB password (default: somestrong) [$PGTT_PGPASSWORD]
        --sslmode=[disable|require] What SSL priority use for connection (default: disable)
        --pgurl=                    PG config DB url [$PGTT_URL]
        --init                      Initialize database schema and exit. Can be used with --upgrade
        --upgrade                   Upgrade database to the latest version
        --no-program-tasks            Disable executing of PROGRAM tasks [$PGTT_NOPROGRAMTASKS]
 


Contributing
------------

If you want to contribute to **pg_timetable** and help make it better, feel free to open an 
`issue <https://github.com/cybertec-postgresql/pg_timetable/issues>`_ or even consider submitting a 
`pull request <https://github.com/cybertec-postgresql/pg_timetable/pulls>`_.

Support
------------

For professional support, please contact `Cybertec <https://www.cybertec-postgresql.com/>`_.


Authors
---------

`Pavlo Golub <https://github.com/pashagolub>`_ and `Hans-Jürgen Schönig <https://github.com/postgresql007>`_.
