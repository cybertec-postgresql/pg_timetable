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
# Load YAML chains
pg_timetable --file chains.yaml postgresql://user:pass@host/db

# Validate YAML without importing
pg_timetable --file chains.yaml --validate

# Replace existing chains with same names
pg_timetable --file chains.yaml --replace postgresql://user:pass@host/db
```

## YAML Format

### Basic Structure

```yaml
chains:
  - name: "chain-name"                 # Required: unique identifier
    schedule: "* * * * *"              # Required: cron format
    live: true                         # Optional: enable/disable chain
    max_instances: 1                   # Optional: max parallel executions
    timeout: 30000                     # Optional: timeout in milliseconds
    self_destruct: false               # Optional: delete after success
    exclusive: false                   # Optional: pause other chains while running
    client_name: "worker-1"            # Optional: restrict to specific client
    on_error: "SELECT log_error($1)"   # Optional: error handling SQL
    tasks:                             # Required: array of tasks
      - name: "task-name"              # Optional: task description
        kind: "SQL"                    # Optional: SQL, PROGRAM, or BUILTIN
        command: "SELECT now()"        # Required: command to execute
        run_as: "postgres"             # Optional: role for SET ROLE
        connect_string: "postgresql://user@host/otherdb"  # Optional: different database connection
        ignore_error: false            # Optional: continue on error
        autonomous: false              # Optional: run outside transaction
        timeout: 5000                  # Optional: task timeout in ms
        parameters:                    # Optional: task parameters, each entry causes separate execution
          - ["value1", 42]             # Parameters for SQL tasks are arrays of values
```

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

# BUILTIN: Sleep task (integer values)
- name: "sleep-task"
  kind: "BUILTIN"
  command: "Sleep"
  parameters:
    - 5    # Sleep for 5 seconds
    - 10   # Then sleep for 10 seconds

# BUILTIN: Log task (string or object values)
- name: "log-task"
  kind: "BUILTIN"
  command: "Log"
  parameters:
    - "WARNING: Simple message"
    - {"level": "WARNING", "details": "Object message"}

# BUILTIN: SendMail task (complex object)
- name: "mail-task"
  kind: "BUILTIN"
  command: "SendMail"
  parameters:
    - username: "user@example.com"
      password: "password123"
      serverhost: "smtp.example.com"
      serverport: 587
      senderaddr: "user@example.com"
      toaddr: ["recipient@example.com"]
      subject: "Notification"
      msgbody: "<p>Hello User</p>"
      contenttype: "text/html; charset=UTF-8"
```

### Examples

#### Simple SQL Job

```yaml
chains:
  - name: "daily-cleanup"
    schedule: "0 2 * * *"  # 2 AM daily
    live: true
    
    tasks:
      - name: "vacuum-tables"
        command: "VACUUM ANALYZE"
```

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

#### Program Tasks  

```yaml
chains:
  - name: "backup-job"
    schedule: "0 3 * * 0"  # Sunday 3 AM
    live: true
    client_name: "backup-worker"
    
    tasks:
      - name: "database-backup"
        kind: "PROGRAM"
        command: "pg_dump"
        parameters:
          - ["-h", "localhost", "-U", "postgres", "-d", "mydb", "-f", "/backups/mydb.sql"]
        timeout: 3600000  # 1 hour
        
      - name: "compress-backup"
        kind: "PROGRAM" 
        command: "gzip"
        parameters: 
          - ["/backups/mydb.sql"]
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

## Validation

YAML files are validated when loaded:

- **Syntax**: Valid YAML format
- **Structure**: Required fields present
- **Cron**: Valid 5-field cron expressions  
- **Task kinds**: Must be SQL, PROGRAM, or BUILTIN
- **Timeouts**: Non-negative integers

Use `--validate` to check files without importing:

```bash
pg_timetable --file chains.yaml --validate
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

## Best Practices

### Naming Conventions

- Use descriptive, kebab-case names
- Include environment in name for clarity
- Group related chains in same file

### Documentation

- Use YAML comments to document complex logic
- Include purpose and dependencies in task names
- Document parameter meanings

```yaml
chains:
  - name: "etl-sales-data"
    # Processes daily sales data from external API
    # Depends on: external API availability, sales_raw table
    schedule: "0 2 * * *"
    
    tasks:
      - name: "extract-from-api"
        # Fetches last 24h of sales data from REST API
        command: "SELECT fetch_sales_data($1)"
        parameters: ["yesterday"]
```

### Testing

- Always validate YAML before deployment
- Test with `--validate` flag
- Use non-live chains for testing
- Keep backups of working configurations

### Version Control

- Store YAML files in version control
- Use meaningful commit messages
- Tag releases for production deployments
- Review changes before merging

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
