Installation
================================================

**pg_timetable** is compatible with the latest supported `PostgreSQL versions <https://www.postgresql.org/support/versioning/>`_: 11, 12, 13 (stable); 14 (dev).

.. note::

    .. raw:: html

        <details>
        <summary>If you want to use pg_timetable with older versions (9.5, 9.6 and 10)...</summary>
        please, execute this SQL script before running pg_timetable:

    .. code-block:: SQL

            CREATE OR REPLACE FUNCTION starts_with(text, text)
            RETURNS bool AS 
            $$
            SELECT 
                CASE WHEN length($2) > length($1) THEN 
                    FALSE 
                ELSE 
                    left($1, length($2)) = $2 
                END
            $$
            LANGUAGE SQL
            IMMUTABLE STRICT PARALLEL SAFE
            COST 5;

    .. raw:: html

        </details>


Official release packages
------------------------------------------------

You may find binary package for your platform on the official `Releases <https://github.com/cybertec-postgresql/pg_timetable/releases>`_ page. Right now **Windows**, **Linux** and **macOS** packages are available.

Docker
------------------------------------------------

The official docker image can be found here: https://hub.docker.com/r/cybertecpostgresql/pg_timetable

.. note:: 

    The ``latest`` tag is up to date with the `master` branch thanks to `his github action <https://github.com/cybertec-postgresql/pg_timetable/blob/master/.github/workflows/docker.yml>`_. In production you probably want to use the latest `stable tag <https://hub.docker.com/r/cybertecpostgresql/pg_timetable/tags>`_.

Run pg_timetable in Docker:

.. code-block:: console

    docker run --rm \
    cybertecpostgresql/pg_timetable:latest \
    -h 10.0.0.3 -p 54321 -c worker001

Run pg_timetable in Docker with Environment variables:

.. code-block:: console

    docker run --rm \
    -e PGTT_PGHOST=10.0.0.3 \
    -e PGTT_PGPORT=54321 \
    cybertecpostgresql/pg_timetable:latest \
    -c worker001

Build from sources
------------------------------------------------

1. Download and install `Go <https://golang.org/doc/install>`_ on your system.
#. Clone **pg_timetable** repo::

    $ git clone https://github.com/cybertec-postgresql/pg_timetable.git
    $ cd pg_timetable

#. Run **pg_timetable**::
    
    $ go run main.go --dbname=dbname --clientname=worker001 --user=scheduler --password=strongpwd

#. Alternatively, build a binary and run it::

    $ go build
    $ ./pg_timetable --dbname=dbname --clientname=worker001 --user=scheduler --password=strongpwd

#. (Optional) Run tests in all sub-folders of the project::

    $ go test -failfast -timeout=300s -count=1 -parallel=1 ./...

