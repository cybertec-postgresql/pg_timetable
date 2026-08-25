# Installation

**pg_timetable** is compatible with all supported [PostgreSQL versions](https://www.postgresql.org/support/versioning/).

!!! note "PostgreSQL extensions"

    **No extension is required to run pg_timetable.** The secret store
    (`timetable.secret`, introduced by migration `00820`) is the only
    feature with an extension dependency: it uses `pgcrypto`'s
    `pgp_sym_encrypt` / `pgp_sym_decrypt`. `pgcrypto` is installed by
    whoever deploys the database — pg_timetable never installs, requires,
    or probes for it. A database without `pgcrypto` runs pg_timetable
    normally with only the secret store unavailable.

    Since PostgreSQL 13, `pgcrypto` is a **trusted** extension and can
    be installed by any role holding `CREATE` on the database, so
    managed services (RDS, Azure, Cloud SQL, Supabase and similar)
    need no superuser.

## Official release packages

You may find binary package for your platform on the official [Releases](https://github.com/cybertec-postgresql/pg_timetable/releases) page. Right now **Windows**, **Linux** and **macOS** packages are available.

## Docker

The official docker image can be found here: <https://hub.docker.com/r/cybertecpostgresql/pg_timetable>
Published tags include a multi-architecture manifest for both `linux/amd64` and `linux/arm64`.

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

## Running as a Windows service

On Windows, **pg_timetable** integrates with the Service Control Manager and ships with built-in service management commands. All commands require an elevated (Administrator) terminal.

By default the service runs as *LocalSystem*. 

Install the service with the same options you would use to run the scheduler interactively — they are persisted into the service command line.
Replace *pgtt* and *pgtt_db* with your database user name and database name. If your postgresql service is remote, make sure to replace localhost:5432 as well.

For LocalSystem (or to share a fixed location), point at the [pgpass.conf](https://www.postgresql.org/docs/current/libpq-pgpass.html) file explicitly either with a `passfile` connection parameter or the `PGPASSFILE` environment variable:

```bat
pg_timetable.exe --service install -c worker001 --log-file=C:\pg_timetable\pg_timetable.log --log-file-rotate "postgresql://pgtt@localhost:5432/pgtt_db?passfile=C:\pg_timetable\pgpass.conf"
```

Manage the service afterwards:

```bat
pg_timetable.exe --service start
pg_timetable.exe --service status
pg_timetable.exe --service stop
pg_timetable.exe --service restart
pg_timetable.exe --service uninstall
```

The service is registered for automatic (delayed) startup and configured to restart after a failure. Use `--service-name` if you install multiple instances, e.g., one per `--clientname`:

```bat
pg_timetable.exe --service install --service-name=pg_timetable_worker002 -c worker002 ...
```

### Running under a dedicated account

To run it under a specific Windows account, use `--service-user` and `--service-password` (the account name may be given as `DOMAIN\user` or `.\user` for a local account):

```bat
pg_timetable.exe --service install --service-user=.\svc_pg_timetable --service-password=S3cret ...
```

- The account requires the **Log on as a service** right; otherwise the service fails to start with error 1069. Grant it via *Local Security Policy → User Rights Assignment* or `secpol.msc`.
- A password may be omitted for accounts that do not need one, e.g., [group managed service accounts](https://learn.microsoft.com/en-us/windows-server/security/group-managed-service-accounts/group-managed-service-accounts-overview) (gMSA).
- Instead of `--service-password`, the password can be supplied through the environment variable `PGTT_SERVICEPASSWORD` to keep it out of shell history and process listings.
- Service management flags are never persisted into the installed service command line, so account credentials do not end up in the registry.

**NOTE:**
A Windows service has no console, so anything written to stdout is discarded. Always configure a log file (`--log-file`, optionally with `--log-file-rotate`) when installing the service.

**NOTE:** When started by the service manager, the working directory is set to the directory containing the executable. Prefer absolute paths for `--config`, `--file` and `--log-file`.

### Storing the database password in pgpass.conf

The database password does not have to be part of the connection string. Like `psql`, **pg_timetable** reads a PostgreSQL password file when no password is given — on Windows this file is `%APPDATA%\postgresql\pgpass.conf`
of the service account you are using. Install the service with a passwordless connection string:

```bat
pg_timetable.exe --service install -c worker001 postgresql://pgtt@localhost:5432/pgtt_db
```

Alternatively, the service can be managed with the standard `sc.exe` utility, e.g.: `sc.exe create pg_timetable start= auto binPath= "\"C:\path\to\pg_timetable.exe\" --clientname=worker001 ..."`


## Build from sources

1. Download and install [Go](https://golang.org/doc/install) on your system.
2. Clone **pg_timetable** repo:

    ```bash
    git clone https://github.com/cybertec-postgresql/pg_timetable.git
    cd pg_timetable
    ```

3. Run **pg_timetable**:

    ```bash
    go run ./cmd/pg_timetable --clientname=worker001 postgresql://scheduler:strongpwd@localhost:5432/dbname
    ```

4. Alternatively, build a binary and run it:

    ```bash
    go build ./cmd/pg_timetable
    ./pg_timetable --clientname=worker001 postgresql://scheduler:strongpwd@localhost:5432/dbname
    ```

5. (Optional) Run tests in all sub-folders of the project:

    ```bash
    psql --command="CREATE USER scheduler PASSWORD 'somestrong'"
    createdb --owner=scheduler timetable
    go test -failfast -timeout=300s -count=1 -p 1 ./...
    ```
