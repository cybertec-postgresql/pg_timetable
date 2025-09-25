# Components

The scheduling in **pg_timetable** encompasses three different abstraction levels to facilitate the reuse with other parameters or additional schedules.

**Command:** The base level, **command**, defines *what* to do.

**Task:** The second level, **task**, represents a chain element (step) to run one of the commands. With **tasks** we define order of commands, arguments passed (if any), and how errors are handled.

**Chain:** The third level represents a connected tasks forming a chain of tasks. **Chain** defines *if*, *when*, and *how often* a job should be executed.

## Command

Currently, there are three different kinds of commands:

### `SQL`
SQL snippet. Starting a cleanup, refreshing a materialized view or processing data.

### `PROGRAM`
External Command. Anything that can be called as an external binary, including shells, e.g. `bash`, `pwsh`, etc. The external command will be called using golang's [exec.CommandContext](https://pkg.go.dev/os/exec#CommandContext).

### `BUILTIN`
Internal Command. A prebuilt functionality included in **pg_timetable**. These include:

* *NoOp*
* *Sleep*
* *Log*
* *SendMail*
* *Download*
* *CopyFromFile*
* *CopyToFile*
* *Shutdown*

## Task

The next building block is a **task**, which simply represents a step in a list of chain commands. An example of tasks combined in a chain would be:

1. Download files from a server
2. Import files
3. Run aggregations
4. Build report
5. Remove the files from disk

!!! note

    All tasks of the chain in **pg_timetable** are executed within one transaction. However, please, pay attention there is no opportunity to rollback `PROGRAM` and `BUILTIN` tasks.

### Table timetable.task

| Field | Type | Description |
|-------|------|-------------|
| `chain_id` | `bigint` | Link to the chain, if `NULL` task considered to be disabled |
| `task_order` | `DOUBLE PRECISION` | Indicates the order of task within a chain |
| `kind` | `timetable.command_kind` | The type of the command. Can be *SQL* (default), *PROGRAM* or *BUILTIN* |
| `command` | `text` | Contains either a SQL command, a path to application or name of the *BUILTIN* command which will be executed |
| `run_as` | `text` | The role as which the task should be executed as |
| `database_connection` | `text` | The connection string for the external database that should be used |
| `ignore_error` | `boolean` | Specify if the next task should proceed after encountering an error (default: `false`) |
| `autonomous` | `boolean` | Specify if the task should be executed out of the chain transaction. Useful for `VACUUM`, `CREATE DATABASE`, `CALL` etc. |
| `timeout` | `integer` | Abort any task within a chain that takes more than the specified number of milliseconds |

!!! warning

    If the **task** has been configured with `ignore_error` set to `true` (the default value is `false`), the worker process will report a success on execution *even if the task within the chain fails*.

As mentioned above, **commands** are simple skeletons (e.g. *send email*, *vacuum*, etc.).
In most cases, they have to be brought to live by passing input parameters to the execution.

### Table timetable.parameter

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | `bigint` | The ID of the task |
| `order_id` | `integer` | The order of the parameter. Several parameters are processed one by one according to the order |
| `value` | `jsonb` | A JSON value containing the parameters |

### Parameter value format

Depending on the **command** kind argument can be represented by different *JSON* values.

#### `SQL`
Schema: `array`

Example:
```sql
'[ "one", 2, 3.14, false ]'::jsonb
```

#### `PROGRAM`
Schema: `array of strings`

Example:
```sql
'["-x", "Latin-ASCII", "-o", "orte_ansi.txt", "orte.txt"]'::jsonb
```

#### `BUILTIN: Sleep`
Schema: `integer`

Example:
```sql
'5' :: jsonb
```

#### `BUILTIN: Log`
Schema: `any`

Examples:
```sql
'"WARNING"'::jsonb
'{"Status": "WARNING"}'::jsonb
```

#### `BUILTIN: SendMail`
Schema: `object`

Example:
```sql
'{
    "username":     "user@example.com",
    "password":     "password",
    "serverhost":   "smtp.example.com",
    "serverport":   587,
    "senderaddr":   "user@example.com",
    "ccaddr":       ["recipient_cc@example.com"],
    "bccaddr":      ["recipient_bcc@example.com"],
    "toaddr":       ["recipient@example.com"],
    "subject":      "pg_timetable - No Reply",
    "attachment":   ["/temp/attachments/Report.pdf","config.yaml"],
    "attachmentdata": [{"name": "File.txt", "base64data": "RmlsZSBDb250ZW50"}],
    "msgbody":      "<h2>Hello User,</h2> <p>check some attachments!</p>",
    "contenttype":   "text/html; charset=UTF-8"
}'::jsonb
```

#### `BUILTIN: Download`
Schema: `object`

Example:
```sql
'{
    "workersnum": 2, 
    "fileurls": ["http://example.com/foo.gz", "https://example.com/bar.csv"], 
    "destpath": "."
}'::jsonb
```

#### `BUILTIN: CopyFromFile`
Schema: `object`

Example:
```sql
'{
    "sql": "COPY location FROM STDIN", 
    "filename": "download/orte_ansi.txt" 
}'::jsonb
```

#### `BUILTIN: CopyToFile`
Schema: `object`

Example:
```sql
'{
    "sql": "COPY location TO STDOUT", 
    "filename": "download/location.txt" 
}'::jsonb
```

#### `BUILTIN: Shutdown`
*value ignored*

#### `BUILTIN: NoOp`
*value ignored*

## Chain

Once tasks have been arranged, they have to be scheduled as a **chain**. For this, **pg_timetable** builds upon the enhanced **cron**-string, all the while adding multiple configuration options.

### Table timetable.chain

| Field | Type | Description |
|-------|------|-------------|
| `chain_name` | `text` | The unique name of the chain |
| `run_at` | `timetable.cron` | Standard *cron*-style value at Postgres server time zone or `@after`, `@every`, `@reboot` clause |
| `max_instances` | `integer` | The amount of instances that this chain may have running at the same time |
| `timeout` | `integer` | Abort any chain that takes more than the specified number of milliseconds |
| `live` | `boolean` | Control if the chain may be executed once it reaches its schedule |
| `self_destruct` | `boolean` | Self destruct the chain after successful execution. Failed chains will be executed according to the schedule one more time |
| `exclusive_execution` | `boolean` | Specifies whether the chain should be executed exclusively while all other chains are paused |
| `client_name` | `text` | Specifies which client should execute the chain. Set this to `NULL` to allow any client |
| `timeout` | `integer` | Abort a chain that takes more than the specified number of milliseconds |
| `on_error` | — | Holds SQL to execute if an error occurs. If task produced an error is marked with `ignore_error` then nothing is done |

!!! note

    All chains in **pg_timetable** are scheduled at the PostgreSQL server time zone.
    You can change the [timezone](https://www.postgresql.org/docs/current/datatype-datetime.html#DATATYPE-TIMEZONES) 
    for the **current session** when adding new chains, e.g.
    
    ```sql
    SET TIME ZONE 'UTC';
    
    -- Run VACUUM at 00:05 every day in August UTC
    SELECT timetable.add_job('execute-func', '5 0 * 8 *', 'VACUUM');
    ```