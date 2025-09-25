# Database Schema

**pg_timetable** is a database driven application. During the first start the necessary schema is created if absent.

## Main tables and objects

```sql
--8<-- "internal/pgengine/sql/ddl.sql"
```

## Jobs related functions

```sql
--8<-- "internal/pgengine/sql/job_functions.sql"
```

## Сron related functions

```sql
--8<-- "internal/pgengine/sql/cron_functions.sql"
```

## ER-Diagram

![Database Schema](timetable_schema.png)

*ER-Diagram showing the database structure*