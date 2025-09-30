# YAML Chain Definition Format for pg_timetable

This document defines the YAML format for defining chains of scheduled tasks in pg_timetable.

## YAML Schema

```yaml
# Top-level structure
chains:
  - name: "chain-name"                        # Required: chain_name (TEXT, unique)
    schedule: "* * * * *"                     # Required: run_at (cron format)
    live: true                                # Optional: live (BOOLEAN), default: false
    max_instances: 1                          # Optional: max_instances (INTEGER)
    timeout: 30000                            # Optional: timeout in milliseconds (INTEGER)
    self_destruct: false                      # Optional: self_destruct (BOOLEAN), default: false
    exclusive: false                          # Optional: exclusive_execution (BOOLEAN), default: false  
    client_name: "worker-1"                   # Optional: client_name (TEXT)
    on_error: "SELECT log_error()"            # Optional: on_error SQL (TEXT)
    
    tasks:                                                # Required: array of tasks
      - name: "task-1"                                    # Optional: task_name (TEXT)
        kind: "SQL"                                       # Optional: kind (SQL|PROGRAM|BUILTIN), default: SQL
        command: "SELECT $1, $2"                          # Required: command (TEXT)
        parameters:                                       # Optional: parameters (array of execution parameters)
          - ["value1", 42]                                # First execution with these parameters
          - ["value2", 99]                                # Second execution with different parameters
        run_as: "postgres"                                # Optional: run_as (TEXT) - role for SET ROLE
        connect_string: "postgresql://user@host/otherdb"  # Optional: database_connection (TEXT)
        ignore_error: false                               # Optional: ignore_error (BOOLEAN), default: false
        autonomous: false                                 # Optional: autonomous (BOOLEAN), default: false
        timeout: 5000                                     # Optional: timeout in milliseconds (INTEGER)
        
      - name: "task-2"
        kind: "PROGRAM"
        command: "bash"
        parameters: ["-c", "echo hello"]
        ignore_error: true
```

## Field Mappings

### Chain Level

| YAML Field | DB Column | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `name` | `chain_name` | TEXT | **required** | Unique chain identifier |
| `schedule` | `run_at` | cron | **required** | Cron-style schedule |
| `live` | `live` | BOOLEAN | `false` | Whether chain is active |
| `max_instances` | `max_instances` | INTEGER | `null` | Max parallel instances |
| `timeout` | `timeout` | INTEGER | `0` | Chain timeout (ms) |
| `self_destruct` | `self_destruct` | BOOLEAN | `false` | Delete after success |
| `exclusive` | `exclusive_execution` | BOOLEAN | `false` | Pause other chains |
| `client_name` | `client_name` | TEXT | `null` | Restrict to specific client |
| `on_error` | `on_error` | TEXT | `null` | Error handling SQL |

### Task Level  

| YAML Field | DB Column | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `name` | `task_name` | TEXT | `null` | Task description |
| `kind` | `kind` | ENUM | `'SQL'` | Command type (SQL/PROGRAM/BUILTIN) |
| `command` | `command` | TEXT | **required** | Command to execute |
| `parameters` | via `timetable.parameter` | Array of any | `null` | Array of parameter values stored as individual JSONB rows with order_id |
| `run_as` | `run_as` | TEXT | `null` | Role for SET ROLE |
| `connect_string` | `database_connection` | TEXT | `null` | Connection string |
| `ignore_error` | `ignore_error` | BOOLEAN | `false` | Continue on error |
| `autonomous` | `autonomous` | BOOLEAN | `false` | Execute outside transaction |
| `timeout` | `timeout` | INTEGER | `0` | Task timeout (ms) |

## Task Ordering

Tasks are ordered sequentially within a chain based on their array position. The system will automatically assign appropriate `task_order` values with spacing (e.g., 10, 20, 30) to allow future insertions.

## Examples

### Simple SQL Job

```yaml
chains:
  - name: "daily-report"
    schedule: "0 9 * * *"  # 9 AM daily
    live: true
    tasks:
      - name: "generate-report"
        command: "CALL generate_daily_report()"
```

### Multi-task Chain

```yaml
chains:
  - name: "etl-pipeline"
    schedule: "0 2 * * *"  # 2 AM daily
    live: true
    max_instances: 1
    timeout: 3600000  # 1 hour
    
    tasks:
      - name: "extract-data"
        command: "SELECT extract_sales_data($1)"
        parameters: ["2023-01-01"]
        
      - name: "transform-data"  
        command: "CALL transform_sales_data()"
        autonomous: true
        
      - name: "load-data"
        command: "CALL load_to_warehouse()"
        ignore_error: false
```

### Program Task

```yaml  
chains:
  - name: "backup-job"
    schedule: "0 3 * * 0"  # Sunday 3 AM
    live: true
    
    tasks:
      - name: "pg-dump"
        kind: "PROGRAM"
        command: "pg_dump"
        parameters: 
          - ["-h", "localhost", "-U", "postgres", "-d", "mydb", "-f", "/backups/mydb.sql"]
```

## Validation Rules

1. **Required Fields**: `name`, `schedule`, `tasks`, and `command` for each task
2. **Unique Names**: Chain names must be unique across the database
3. **Valid Cron**: Schedule must be valid cron format (5 fields)
4. **Valid Kind**: Task kind must be one of: SQL, PROGRAM, BUILTIN
5. **Parameter Types**: Parameters can be any JSON-compatible type (strings, numbers, booleans, arrays, objects) and are stored as individual JSONB values
6. **Timeout Values**: Must be non-negative integers (milliseconds)
