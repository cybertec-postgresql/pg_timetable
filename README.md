[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](https://opensource.org/licenses/MIT)
![](https://github.com/cybertec-postgresql/pg_timetable/workflows/Go%20Build%20&%20Test/badge.svg)
[![Coverage Status](https://coveralls.io/repos/github/cybertec-postgresql/pg_timetable/badge.svg?branch=master&service=github)](https://coveralls.io/github/cybertec-postgresql/pg_timetable?branch=master)
[![Documentation Status](https://readthedocs.org/projects/pg-timetable/badge/?version=master)](https://pg-timetable.readthedocs.io/en/master/?badge=master)
[![Release](https://img.shields.io/github/v/release/cybertec-postgresql/pg_timetable?include_prereleases)](https://github.com/cybertec-postgresql/pg_timetable/releases)
[![Github All Releases](https://img.shields.io/github/downloads/cybertec-postgresql/pg_timetable/total?style=flat-square)](https://github.com/cybertec-postgresql/pg_timetable/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/cybertecpostgresql/pg_timetable)](https://hub.docker.com/r/cybertecpostgresql/pg_timetable)
[![Go Report Card](https://goreportcard.com/badge/github.com/cybertec-postgresql/pg_timetable)](https://goreportcard.com/report/github.com/cybertec-postgresql/pg_timetable)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)



pg_timetable: Advanced scheduling for PostgreSQL
================================================

**pg_timetable** is an advanced standalone job scheduler for PostgreSQL, offering many advantages over traditional schedulers such as **cron** and others.
It is completely database driven and provides a couple of advanced concepts. It allows you to schedule PostgreSQL commands, system programs and built-in operations:

```sql
-- Run public.my_func() at 00:05 every day in August:
SELECT timetable.add_job('execute-func', '5 0 * 8 *', 'SELECT public.my_func()');

-- Run VACUUM at minute 23 past every 2nd hour from 0 through 20 every day:
SELECT timetable.add_job('run-vacuum', '23 0-20/2 * * *', 'VACUUM');

-- Refresh materialized view every 2 hours:
SELECT timetable.add_job('refresh-matview', '@every 2 hours', 
  'REFRESH MATERIALIZED VIEW public.mat_view');

-- Clear log table after pg_timetable restart:
SELECT timetable.add_job('clear-log', '@reboot', 'TRUNCATE public.log');

-- Reindex at midnight on Sundays with reindexdb utility:

--  using default database under default user (no command line arguments)
SELECT timetable.add_job('reindex-job', '0 0 * * 7', 'reindexdb', job_kind := 'PROGRAM');

--  specifying target database and tables, and be verbose
SELECT timetable.add_job('reindex-job', '0 0 * * 7', 'reindexdb',
          '["--table=foo", "--dbname=postgres", "--verbose"]'::jsonb, 'PROGRAM');

--  passing password using environment variable through bash shell
SELECT timetable.add_job('reindex-job', '0 0 * * 7', 'bash',
    '["-c", "PGPASSWORD=5m3R7K4754p4m reindexdb -U postgres -h 192.168.0.221 -v'::jsonb,
    'PROGRAM');    
```      
## Documentation

https://pg-timetable.readthedocs.io/

## Main features
- Tasks can be arranged in chains
- Each task executes SQL, built-in or executable command
- Parameters can be passed to tasks
- Missed chains (possibly due to downtime) can be retried automatically
- Support for configurable repetitions
- Builtin tasks such as sending emails, downloading, importing files, etc.
- Fully database driven configuration
- Full support for database driven logging
- Enhanced cron-style scheduling
- Optional concurrency protection

## [Installation](https://pg-timetable.readthedocs.io/en/master/installation.html)

Complete installation guide can be found in the [documentation](https://pg-timetable.readthedocs.io/en/master/installation.html).

Possible choices are:
- official [release packages](https://github.com/cybertec-postgresql/pg_timetable/releases);
- [Docker images](https://hub.docker.com/r/cybertecpostgresql/pg_timetable);
- [build from sources](https://pg-timetable.readthedocs.io/en/master/installation.html#build-from-sources).

## [Quick Start](https://pg-timetable.readthedocs.io/en/master/README.html#quick-start)

Complete usage guide can be found in the [documentation](https://pg-timetable.readthedocs.io/en/master/basic_jobs.html).

1. Download **pg_timetable** executable

2. Make sure your **PostgreSQL** server is up and running and has a role with `CREATE` privilege for a target database, e.g.
```sql
    my_database=> CREATE ROLE scheduler PASSWORD 'somestrong';
    my_database=> GRANT CREATE ON DATABASE my_database TO scheduler;
```
3. Create a new job, e.g. run `VACUUM` each night at 00:30
```sql
    my_database=> SELECT timetable.add_job('frequent-vacuum', '30 0 * * *', 'VACUUM');
    add_job
    ---------
          3
    (1 row)
```
4. Run the pg_timetable
```terminal
    # pg_timetable postgresql://scheduler:somestrong@localhost/my_database --clientname=vacuumer
```
5. PROFIT!

## Supported Environments
| Cloud Service    | Supported | PostgreSQL Version | Supported | OS | Supported |
| ---------------- |:---------:| ------------------ |:---------:| -- |:---------:|
| [Alibaba Cloud]  | ✅       | [14 (devel)]   | ✅ | Linux    | ✅ |
| [Amazon RDS]     | ✅       | [13 (current)] | ✅ | Darwin   | ✅ |
| [Amazon Aurora]  | ✅       | [12]           | ✅ | Windows  | ✅ |
| [Azure]          | ✅       | [11]           | ✅ | FreeBSD\*| ✅ |
| [Citus Cloud]    | ✅       | [10]           | ✅ | NetBSD\* | ✅ |
| [Crunchy Bridge] | ✅       | [9.6]          | ✅ | OpenBSD\*| ✅ | 
| [DigitalOcean]   | ✅       |                |    | Solaris\* | ✅ |
| [Google Cloud]   | ✅       |
| [Heroku]         | ✅       |
| [Supabase]       | ✅       |

\* - there are no official release binaries made for these OSes. One would need to [build them from sources](https://pg-timetable.readthedocs.io/en/master/installation.html#build-from-sources).

\** - previous PostgreSQL versions may and should work smoothly. Only [officially supported PostgreSQL versions](https://www.postgresql.org/support/versioning/) are listed in this table.
      
[Alibaba Cloud]: https://www.alibabacloud.com/help/doc-detail/96715.htm
[Amazon RDS]: https://aws.amazon.com/rds/postgresql/
[Amazon Aurora]: https://aws.amazon.com/rds/aurora/
[Azure]: https://azure.microsoft.com/en-us/services/postgresql/
[Citus Cloud]: https://www.citusdata.com/product/cloud
[Crunchy Bridge]: https://www.crunchydata.com/products/crunchy-bridge/
[DigitalOcean]: https://www.digitalocean.com/products/managed-databases/
[Google Cloud]: https://cloud.google.com/sql/docs/postgres/
[Heroku]: https://elements.heroku.com/addons/heroku-postgresql
[Supabase]: https://supabase.io/docs/guides/database
[14 (devel)]: https://www.postgresql.org/docs/devel/index.html
[13 (current)]: https://www.postgresql.org/docs/13/index.html
[12]: https://www.postgresql.org/docs/12/index.html
[11]: https://www.postgresql.org/docs/11/index.html
[10]: https://www.postgresql.org/docs/10/index.html
[9.6]: https://www.postgresql.org/docs/9.6/index.html
      
[Alibaba Cloud]: https://www.alibabacloud.com/help/doc-detail/96715.htm
[Amazon RDS]: https://aws.amazon.com/rds/postgresql/
[Amazon Aurora]: https://aws.amazon.com/rds/aurora/
[Azure]: https://azure.microsoft.com/en-us/services/postgresql/
[Citus Cloud]: https://www.citusdata.com/product/cloud
[Crunchy Bridge]: https://www.crunchydata.com/products/crunchy-bridge/
[DigitalOcean]: https://www.digitalocean.com/products/managed-databases/
[Google Cloud]: https://cloud.google.com/sql/docs/postgres/
[Heroku]: https://elements.heroku.com/addons/heroku-postgresql
[Supabase]: https://supabase.io/docs/guides/database
[14 (devel)]: https://www.postgresql.org/docs/devel/index.html
[13 (current)]: https://www.postgresql.org/docs/13/index.html
[12]: https://www.postgresql.org/docs/12/index.html
[11]: https://www.postgresql.org/docs/11/index.html
[10]: https://www.postgresql.org/docs/10/index.html
[9.6]: https://www.postgresql.org/docs/9.6/index.html

## Contributing

If you want to contribute to **pg_timetable** and help make it better, feel free to open an [issue][issue] or even consider submitting a [pull request][PR].

[issue]: https://github.com/cybertec-postgresql/pg_timetable/issues
[PR]: https://github.com/cybertec-postgresql/pg_timetable/pulls

## Support

For professional support, please contact [Cybertec](https://www.cybertec-postgresql.com/).

## Authors

- Implementation: [Pavlo Golub](https://github.com/pashagolub)
- Initial idea and draft design: [Hans-Jürgen Schönig](https://github.com/postgresql007)
