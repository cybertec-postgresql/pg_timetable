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
        live: true                                        # Optional: live (BOOLEAN), default: true; set false to skip the task
        
      - name: "task-2"
        kind: "PROGRAM"
        command: "bash"
        parameters: [["-c", "echo hello"]]
        ignore_error: true
```

## Field Mappings

### Chain Level

| YAML Field | DB Column | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `name` | `chain_name` | TEXT | **required** | Unique chain identifier |
| `schedule` | `run_at` | cron | **required** | Cron-style schedule. If omitted or empty, defaults to `* * * * *` (every minute) |
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
| `live` | `live` | BOOLEAN | `true` | Whether task is executed; disabled tasks are skipped |

## Task Parameters

For SQL and PROGRAM tasks, each `parameters` entry is an array of arguments for one
execution. For example, `parameters: [["value"]]` runs once with one argument, while
`parameters: ["value"]` stores a scalar and fails when the task tries to read its
argument array. BUILTIN commands use their command-specific parameter formats.

## Task Ordering

Tasks are ordered sequentially within a chain based on their array position. The system will automatically assign appropriate `task_order` values with spacing (e.g., 10, 20, 30) to allow future insertions.

## Examples

### Simple SQL Job

```yaml
--8<-- "samples/yaml/DailyReport.yaml"
```

### Multi-task Chain

```yaml
--8<-- "samples/yaml/MultiTask.yaml"
```

### Program Task

```yaml
--8<-- "samples/yaml/ProgramTask.yaml"
```

## Validation Rules

1. **Required Fields**: `name`, `schedule`, `tasks`, and `command` for each task
2. **Unique Names**: Chain names must be unique across the database
3. **Valid Cron**: Schedule must be valid cron format (5 fields)
4. **Valid Kind**: Task kind must be one of: SQL, PROGRAM, BUILTIN
5. **Parameter Types**: Each parameter entry is stored as an individual JSONB value; SQL and PROGRAM entries must be arrays (see [Task Parameters](#task-parameters))
6. **Timeout Values**: Must be non-negative integers (milliseconds)
