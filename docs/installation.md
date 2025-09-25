# Installation

**pg_timetable** is compatible with all supported [PostgreSQL versions](https://www.postgresql.org/support/versioning/).

!!! note "Older PostgreSQL versions (9.5, 9.6, and 10)"

    If you want to use **pg_timetable** with older versions (9.5, 9.6 and 10), please execute this SQL command before running pg_timetable:

    ```sql
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
    ```

## Official release packages

You may find binary package for your platform on the official [Releases](https://github.com/cybertec-postgresql/pg_timetable/releases) page. Right now **Windows**, **Linux** and **macOS** packages are available.

## Docker

The official docker image can be found here: <https://hub.docker.com/r/cybertecpostgresql/pg_timetable>

!!! note

    The `latest` tag is up to date with the `master` branch thanks to [this github action](https://github.com/cybertec-postgresql/pg_timetable/blob/master/.github/workflows/docker.yml). In production you probably want to use the latest [stable tag](https://hub.docker.com/r/cybertecpostgresql/pg_timetable/tags).

Run **pg_timetable** in Docker:

```bash
docker run --rm \
cybertecpostgresql/pg_timetable:latest \
-h 10.0.0.3 -p 54321 -c worker001
```

Run **pg_timetable** in Docker with Environment variables:

```bash
docker run --rm \
-e PGTT_PGHOST=10.0.0.3 \
-e PGTT_PGPORT=54321 \
cybertecpostgresql/pg_timetable:latest \
-c worker001
```

## Build from sources

1. Download and install [Go](https://golang.org/doc/install) on your system.
2. Clone **pg_timetable** repo:

    ```bash
    git clone https://github.com/cybertec-postgresql/pg_timetable.git
    cd pg_timetable
    ```

3. Run **pg_timetable**:

    ```bash
    go run main.go --clientname=worker001 postgresql://scheduler:strongpwd@localhost:5432/dbname
    ```

4. Alternatively, build a binary and run it:

    ```bash
    go build
    ./pg_timetable --clientname=worker001 postgresql://scheduler:strongpwd@localhost:5432/dbname
    ```

5. (Optional) Run tests in all sub-folders of the project:

    ```bash
    psql --command="CREATE USER scheduler PASSWORD 'somestrong'"
    createdb --owner=scheduler timetable
    go test -failfast -timeout=300s -count=1 -p 1 ./...
    ```
