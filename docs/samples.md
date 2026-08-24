# Samples

## Create a basic one-step chain with parameters

Use a CTE to insert directly into the **timetable** schema tables:

```sql
--8<-- "samples/Basic.sql"
```

## Send an email from a chain

Check whether there are emails to send, send them, and log the execution status. No setup is
required beyond specifying parameters at chain-creation time:

```sql
--8<-- "samples/Mail.sql"
```

## Download, transform, and import data in one chain

Use a `DO` statement to insert a three-step chain directly into the **timetable** schema tables:

```sql
--8<-- "samples/Download.sql"
```

## Run a task in an autonomous transaction

Run a task outside the chain's transaction for operations that cannot participate in one, e.g.
*CREATE DATABASE*, *REINDEX*, *VACUUM*, *CREATE TABLESPACE*:

```sql
--8<-- "samples/Autonomous.sql"
```

## Shut down the scheduler and terminate the session

Use the built-in shutdown task to control maintenance windows, restart the scheduler for
updates, or stop the session before dropping the database:

```sql
--8<-- "samples/Shutdown.sql"
```

## Access the previous task's result code and output from the next task

Check the return code and output of a previous task — only possible when that task has
`ignore_error = true`, otherwise the scheduler stops the chain on failure. This sample also
calculates the count of failed, successful, and total tasks executed, and the resulting success
ratio:

```sql
--8<-- "samples/ManyTasks.sql"
```

For the secret store, see [Use the Secret Store](how-to-use-secret-store.md).
