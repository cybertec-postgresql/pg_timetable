# The Scheduling Model

The scheduling in **pg_timetable** encompasses three different abstraction levels to facilitate the reuse with other parameters or additional schedules.

**Command:** The base level, **command**, defines *what* to do.

**Task:** The second level, **task**, represents a chain element (step) to run one of the commands. With **tasks** we define order of commands, arguments passed (if any), and how errors are handled.

**Chain:** The third level represents connected tasks forming a chain of tasks. **Chain** defines *if*, *when*, and *how often* a job should be executed. A chain with a `NULL` schedule (no `run_at` value) is started at every scheduler loop tick — every 60 seconds by default — so it effectively runs every minute.

The exact fields, kinds, and parameter formats for each level are documented in the [Commands, Tasks, and Chains Reference](reference-commands-tasks-chains.md).
