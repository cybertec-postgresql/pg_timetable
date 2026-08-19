# YAML Chain Configuration Guide

This guide explains how to use YAML files to define pg_timetable chains as an alternative to SQL-based configuration.

## Overview

YAML chain definitions provide a human-readable way to create scheduled task chains without writing SQL. Benefits include:

- Creating complex multi-step workflows with clear structure
- Version controlling your chain configurations
- Easy review and modification of scheduled tasks
- Sharing chain templates across environments

## Basic Usage

```bash
# Load a single YAML file
pg_timetable --file chains.yaml postgresql://user:pass@host/db

# Load several startup files in order
pg_timetable --file bootstrap.sql --file chains/base.yaml --file chains/prod.yaml ******host/db

# Validate YAML without importing
pg_timetable --file chains.yaml --validate

# Replace existing chains with same names
pg_timetable --file chains.yaml --replace postgresql://user:pass@host/db
```

Each `--file` value is processed in the order provided, so you can mix SQL bootstrap scripts and YAML chain definitions in one startup command.

## YAML Format

See the [YAML Chain Schema](yaml-format.md) reference for the complete field list and defaults. The patterns below show common authoring tasks.

### Task Parameters

Each task can have multiple parameter entries, with each entry causing a separate execution:

```yaml
# SQL task parameters (arrays of values)
- name: "sql-task"
  kind: "SQL"
  command: "SELECT $1, $2, $3, $4"
  parameters:
    - ["one", 2, 3.14, false]    # First execution
    - ["two", 4, 6.28, true]     # Second execution

# PROGRAM task parameters (arrays of command-line arguments)
- name: "program-task"
  kind: "PROGRAM"
  command: "iconv"
  parameters:
    - ["-x", "Latin-ASCII", "-o", "file1.txt", "input1.txt"]
    - ["-x", "UTF-8", "-o", "file2.txt", "input2.txt"]
```

`BUILTIN` commands (`Sleep`, `Log`, `SendMail`, `Download`, `CopyFromFile`, `CopyToFile`,
`CopyFromProgram`, `CopyToProgram`) take the same parameter value in YAML as in SQL — only the
surrounding syntax differs. For every command's exact JSON shape and a worked example, see
[Parameter value format](reference-commands-tasks-chains.md#parameter-value-format).

### Examples

For a minimal single-task example, see [YAML Chain Schema](yaml-format.md#simple-sql-job).

#### Multi-Step Chain

```yaml
chains:
  - name: "data-pipeline"
    schedule: "0 1 * * *"  # 1 AM daily
    live: true
    max_instances: 1
    timeout: 7200000  # 2 hours
    
    tasks:
      - name: "extract"
        command: |
          CREATE TEMP TABLE temp_data AS
          SELECT * FROM source_table 
          WHERE date >= CURRENT_DATE - INTERVAL '1 day'
          
      - name: "validate"
        command: |
          DO $$
          BEGIN
            IF (SELECT COUNT(*) FROM temp_data) = 0 THEN
              RAISE EXCEPTION 'No data to process';
            END IF;
          END $$
          
      - name: "transform"
        command: "CALL transform_data_procedure()"
        autonomous: true
        
      - name: "load"
        command: "INSERT INTO target_table SELECT * FROM temp_data"
```

#### Multiple Chains in One File

```yaml
chains:
  # Monitoring chain
  - name: "health-check"
    schedule: "*/15 * * * *"  # Every 15 minutes
    live: true
    
    tasks:
      - command: "SELECT check_database_health()"
      
  # Cleanup chain  
  - name: "hourly-cleanup"
    schedule: "0 * * * *"  # Every hour
    live: true
    
    tasks:
      - command: "DELETE FROM logs WHERE created_at < now() - interval '7 days'"
```

#### Disabling a Task Without Removing It

```yaml
chains:
  - name: "maintenance-window"
    schedule: "0 1 * * *"
    live: true

    tasks:
      - name: "pre-check"
        command: "SELECT run_precheck()"
      - name: "paused-step"
        command: "SELECT run_optional_step()"
        live: false
      - name: "post-check"
        command: "SELECT run_postcheck()"
```

Tasks with `live: false` remain part of the chain definition but are skipped until re-enabled.

## Advanced Features

### Error Handling

Control error behavior with `ignore_error` and `on_error`:

```yaml
chains:
  - name: "resilient-chain"
    on_error: |
      SELECT pg_notify('monitoring', 
            format('{"ConfigID": %s, "Message": "Something bad happened"}', 
                current_setting('pg_timetable.current_chain_id')::bigint))
    
    tasks:
      - name: "risky-task"
        command: "SELECT might_fail()"
        ignore_error: true  # Continue chain execution even if this task fails
        
      - name: "cleanup-task"
        command: "SELECT cleanup()"  # Always runs, even if previous task failed
```

### Transaction Control

Use `autonomous: true` for tasks that need to run outside the main transaction:

```yaml
tasks:
  - name: "vacuum-task"
    command: "VACUUM FULL heavy_table"
    autonomous: true  # Required for VACUUM FULL
    
  - name: "create-database"
    command: "CREATE DATABASE new_db"
    autonomous: true  # CREATE DATABASE requires autonomous transaction
```

### Remote Databases

Execute tasks on different databases:

```yaml
tasks:
  - name: "cross-database-task"
    command: "SELECT sync_data()"
    connect_string: "postgresql://user:pass@other-host/other-db"
```

## Migration from SQL

### Converting Existing Chains

To convert SQL-based chains to YAML:

1. **Query chain and tasks information**:

   ```sql
   SELECT *
   FROM timetable.chain c 
   WHERE c.chain_name = 'my-chain';
   
   SELECT t.*
   FROM timetable.task t JOIN 
        timetable.chain c ON t.chain_id = c.chain_id AND c.chain_name = 'my-chain'
   ORDER BY t.task_order;
   ```

2. **Map to YAML format**:
   - `chain_name` → `name`
   - `run_at` → `schedule`  
   - `live` → `live`
   - `max_instances` → `max_instances`
   - Task fields map directly

3. **Test the conversion**:

   ```bash
   pg_timetable --file converted.yaml --validate
   ```

### Example Migration

**Original SQL**:

```sql
SELECT timetable.add_job(
    job_name => 'daily-report',
    job_schedule => '0 9 * * *',
    job_command => 'CALL generate_report()',
    job_live => TRUE
);
```

**Converted YAML**:

```yaml
chains:
  - name: "daily-report"
    schedule: "0 9 * * *"
    live: true
    
    tasks:
      - command: "CALL generate_report()"
```

## Troubleshooting

### Common Issues

**Invalid YAML syntax**:

```text
Error: failed to parse YAML: yaml: line 5: found character that cannot start any token
```

→ Check indentation and quotes

**Invalid cron format**:

```text
Error: invalid cron format: 0 9 * * (expected 5 fields)
```

→ Ensure cron has exactly 5 fields

**Chain already exists**:

```text
Error: chain 'my-chain' already exists (use --replace flag to overwrite)
```

→ Use `--replace` flag or choose different name

**Missing required fields**:

```text
Error: chain 1: chain name is required
```

→ Check all required fields are present

## Secrets

YAML-authored chains do not support `${secret:name}` resolution in v1 — see [Use the Secret Store](how-to-use-secret-store.md#use-secrets-with-yaml-authored-chains) for the two supported workarounds.
