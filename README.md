#### We are actively developing **pg_timetable v4**. Please refer to the [v3 branch](https://github.com/cybertec-postgresql/pg_timetable/tree/master) for previous version documentation and sources.
--------

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](https://opensource.org/licenses/MIT)
![](https://github.com/cybertec-postgresql/pg_timetable/workflows/Go%20Build%20&%20Test/badge.svg)
[![Coverage Status](https://coveralls.io/repos/github/cybertec-postgresql/pg_timetable/badge.svg?branch=v4_dev&service=github)](https://coveralls.io/github/cybertec-postgresql/pg_timetable?branch=v4_dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/cybertec-postgresql/pg_timetable)](https://goreportcard.com/report/github.com/cybertec-postgresql/pg_timetable)
[![Release](https://img.shields.io/github/release/cybertec-postgresql/pg_timetable.svg)](https://github.com/cybertec-postgresql/pg_timetable/releases/latest)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)
[![Docker Pulls](https://img.shields.io/docker/pulls/cybertecpostgresql/pg_timetable)](https://hub.docker.com/r/cybertecpostgresql/pg_timetable)
[![Dependabot Status](https://api.dependabot.com/badges/status?host=github&repo=cybertec-postgresql/pg_timetable)](https://dependabot.com)

pg_timetable: Advanced scheduling for PostgreSQL
================================================

**pg_timetable** is an advanced job scheduler for PostgreSQL, offering many advantages over traditional schedulers such as **cron** and others.
It is completely database driven and provides a couple of advanced concepts.

```terminal
# ./pg_timetable
Usage
  pg_timetable

Application Options:
  -c, --clientname=                   Unique name for application instance [$PGTT_CLIENTNAME]
      --config=                       YAML configuration file
      --no-program-tasks              Disable executing of PROGRAM tasks [$PGTT_NOPROGRAMTASKS]

Connection:
  -h, --host=                         PostgreSQL host (default: localhost) [$PGTT_PGHOST]
  -p, --port=                         PostgreSQL port (default: 5432) [$PGTT_PGPORT]
  -d, --dbname=                       PostgreSQL database name (default: timetable) [$PGTT_PGDATABASE]
  -u, --user=                         PostgreSQL user (default: scheduler) [$PGTT_PGUSER]
      --password=                     PostgreSQL user password [$PGTT_PGPASSWORD]
      --sslmode=[disable|require]     What SSL priority use for connection (default: disable)
      --pgurl=                        PostgreSQL connection URL [$PGTT_URL]

Logging:
      --loglevel=[debug|info|error]   Verbosity level for stdout and log file (default: info)
      --logdblevel=[debug|info|error] Verbosity level for database storing (default: info)
      --logfile=                      File name to store logs
      --logfileformat=[json|text]     Format of file logs (default: json)

Start:
  -f, --file=                         SQL script file to execute during startup
      --init                          Initialize database schema to the latest version and exit. Can
                                      be used with --upgrade
      --upgrade                       Upgrade database to the latest version
      --debug                         Run in debug mode. Only asynchronous chains will be executed

Resource:
      --cronworkers=                  Number of parallel workers for scheduled chains (default: 16)
      --intervalworkers=              Number of parallel workers for interval chains (default: 16)      
```      

## Table of Contents
  - [1. Main features](#1-main-features)
  - [2. Installation](#2-installation)
    - [2.1. Official release packages](#21-official-release-packages)
    - [2.2. Docker](#22-docker)
    - [2.3. Build from sources](#23-build-from-sources)
  - [3. Example usages](#3-example-usages)
  - [4. Database logging and transactions](#4-database-logging-and-transactions)
  - [5. Runtime information](#5-runtime-information)
  - [6. Schema diagram](#6-schema-diagram)
  - [7. Contributing](#7-contributing)
  - [8. Support](#8-support)
  - [9. Authors](#9-authors)

## 1. Main features

- Tasks can be arranged in chains
- A chain can consist of SQL and executables
- Parameters can be passed to chains
- Missed tasks (possibly due to downtime) can be retried automatically
- Support for configurable repetitions
- Builtin tasks such as sending emails, etc.
- Fully database driven configuration
- Full support for database driven logging
- Cron-style scheduling
- Optional concurrency protection

## 2. Installation

pg_timetable is compatible with the latest supported [PostgreSQL versions](https://www.postgresql.org/support/versioning/): 11, 12 and 13. 

<details>
  <summary>If you want to use pg_timetable with older versions (9.5, 9.6 and 10)...</summary>
  
please, execute this SQL script before running pg_timetable:
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
</details>

### 2.1 Official release packages

You may find binary package for your platform on the official [Releases](https://github.com/cybertec-postgresql/pg_timetable/releases) page. Right now `Windows`, `Linux` and `macOS` packages are available.

### 2.2 Docker

The official docker image can be found here: https://hub.docker.com/r/cybertecpostgresql/pg_timetable

The `latest` tag is up to date with the `master` branch thanks to [this github action](https://github.com/cybertec-postgresql/pg_timetable/blob/master/.github/workflows/docker.yml).

CLI:

```sh
docker run --rm \
  cybertecpostgresql/pg_timetable:latest \
  -h 10.0.0.3 -p 54321 -c worker001
```

Environment variables:

```sh
docker run --rm \
  -e PGTT_PGHOST=10.0.0.3 \
  -e PGTT_PGPORT=54321 \
  cybertecpostgresql/pg_timetable:latest \
  -c worker001
```

### 2.3 Build from sources

1. Download and install [Go](https://golang.org/doc/install) on your system.
2. Clone **pg_timetable** using `go get`:
```sh
$ git clone https://github.com/cybertec-postgresql/pg_timetable.git
$ cd pg_timetable
```
3. Run `pg_timetable`:
```sh
$ go run main.go --dbname=dbname --clientname=worker001 --user=scheduler --password=strongpwd
```
Alternatively, build a binary and run it:
```sh
$ go build
$ ./pg_timetable --dbname=dbname --clientname=worker001 --user=scheduler --password=strongpwd
```

4. (Optional) Run tests in all sub-folders of the project:
```sh
$ go test -failfast -timeout=300s -count=1 -parallel=1 ./...
```

### 3 Example usages

A variety of examples can be found in the `/samples` directory.

Create a job with the `timetable.add_job` function. With this function you can add a new one-step chain with a cron-syntax.

| Parameter                   | Type    | Definition                                       | Default |
| :----------------------- | :------ | :----------------------------------------------- |:---------|
| `job_name`     | `text`  | The name of the Task ||
| `job_schedule`        | `timetable.cron`  | Time schedule in сron syntax. `NULL` stands for `'* * * * *'`     ||
| `job_command` | `text`  | The function which will be executed. ||
| `job_client_name`   | `text`  | Specifies which client should execute the chain. Set this to `NULL` to allow any client. |NULL|
| `job_type`     | `text`  | Type of the function `SQL`,`PROGRAM` and `BUILTIN` |SQL|
| `job_max_instances` | `integer` | The amount of instances that this chain may have running at the same time. |NULL|
| `job_live`          | `boolean` | Control if the chain may be executed once it reaches its schedule. |TRUE|
| `job_self_destruct` | `boolean` | Self destruct the chain. |FALSE|
| `job_ignore_errors` | `boolean` | Ignore error during execution. |TRUE|

Run "MyJob" at 00:05 in August.
```SELECT timetable.add_job('execute-func', '5 0 * 8 *', 'SELECT public.my_func()');```

Run `VACUUM` at minute 23 past every 2nd hour from 0 through 20.
```SELECT timetable.add_job('run-vacuum', '23 0-20/2 * * *', 'VACUUM');```
    
## 4. Database logging and transactions

The entire activity of **pg_timetable** is logged in database tables (`timetable.log` and `timetable.execution_log`). Since there is no need to parse files when accessing log data, the representation through an UI can be easily achieved.

Furthermore, this behavior allows a remote host to access the log in a straightforward manner, simplifying large and/or distributed applications.
>Note: Logs are written in a separate transaction, in case the chain fails.

## 5. Runtime information

In order to examine the activity of **pg_timetable**, the table `timetable.run_status` can be queried. It contains information about active jobs and their current parameters.

## 6. Schema diagram

![Schema diagram](timetable_schema.png?raw=true "Schema diagram")

## 7. Contributing

If you want to contribute to **pg_timetable** and help make it better, feel free to open an [issue][issue] or even consider submitting a [pull request][PR].

[issue]: https://github.com/cybertec-postgresql/pg_timetable/issues
[PR]: https://github.com/cybertec-postgresql/pg_timetable/pulls

## 8. Support

For professional support, please contact [Cybertec][cybertec].

[cybertec]: https://www.cybertec-postgresql.com/


## 9. Authors

[Pavlo Golub](https://github.com/pashagolub) and [Hans-Jürgen Schönig](https://github.com/postgresql007).
