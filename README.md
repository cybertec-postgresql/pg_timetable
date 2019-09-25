pg_timetable: Advanced scheduling for PostgreSQL
================================================

**pg_timetable** is an advanced job scheduler for PostgreSQL, offering many advantages over traditional schedulers such as **cron** and others.
It is completely database driven and provides a couple of advanced concepts.

## Table of Contents
  - [1. Main features](#1-main-features)
  - [2. Installation](#2-installation)
  - [3. Features and advanced functionality](#3-features-and-advanced-functionality)
    - [3.1. Base task](#31-base-task)
    - [3.2. Task chain](#32-task-chain)
    	- [3.2.1. Chain execution configuration](#321-chain-execution-configuration)
    	- [3.2.2. Chain execution parameters](#322-chain-execution-parameters)
    - [3.3. Example usages](#33-example-usages)
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

1. Downlod and install [Go](https://golang.org/doc/install) on your system.
2. Clone **pg_timetable** using `go get`:
```sh
$ env GIT_TERMINAL_PROMPT=1 go get github.com/cybertec-postgresql/pg_timetable/
Username for 'https://github.com': <Github Username>
Password for 'https://cyberboy@github.com': <Github Password>
```
3. Run `pg_timetable`:
```sh
$ cd ~/go/src/github.com/cybertec-postgresql/pg_timetable/
$ go run main.go --dbname=dbname --name=worker001 --user=scheduler --password=strongpwd
```
Alternatively, build a binary and run it:
```sh
$ go build
$ ./pg_timetable --dbname=dbname --name=worker001 --user=scheduler --password=strongpwd
```

4. (Optional) Run tests in all sub-folders of the project:
```sh
$ cd ~/go/src/github.com/cybertec-postgresql/pg_timetable/
$ go get github.com/stretchr/testify/
$ go test ./...
```

## 3. Features and advanced functionality

The scheduling in **pg_timetable** encompasses *three* different stages to facilitate the reuse with other parameters or additional schedules.


The first stage, ***base_task***, defines what to do.\
The second stage, ***task_chain***, contains a list of base tasks to run sequentially.\
The third stage consists of the ***chain_execution_config*** and defines *if*, *when*, and *how often* a chain should be executed.

Additionally, to provide the base tasks with parameters and influence their behavior, each entry in a task chain can be accompanied by an ***execution parameter***.

### 3.1. Base task

In **pg_timetable**, the most basic building block is a ***base task***. Currently, there are three different kinds of task:

| Base task kind   | Task kind type | Example                                                                                                                                                             |
| :--------------- | :------------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| SQL snippet      | `SQL`          | Starting a cleanup, refreshing a materialized view or processing data.                                                                                              |
| External program | `SHELL`        | Anything that can be called from the command line.                                                                                                                  |
| Internal Task    | `BUILTIN`      | A prebuilt functionality included in **pg_timetable**. These include: <ul style="margin-top:12px"><li>Sleep</li><li>Log</li><li>SendMail</li><li>Download</li></ul> |

A new base task can be created by inserting a new entry into `timetable.base_task`.

<p align="center">Excerpt of <code>timetable.base_task</code></p>

| Column   | Type                  | Definition                                                              |
| :------- | :-------------------- | :---------------------------------------------------------------------- |
| `name`   | `text`                | The name of the base task.                                              |
| `kind`   | `timetable.task_kind` | The type of the base task. Can be `SQL`(default), `SHELL` or `BUILTIN`. |
| `script` | `text`                | TODO                                                                    |

### 3.2. Task chain

The next building block is a ***chain***, which simply represents a list of tasks. An example would be:
- Start a transaction
- Download files from a server
- Import files
- Run aggregations
- Commit the transaction
- Remove the files from disk

Through chains, **pg_timetable** creates the ability to span transactions over more than just one task.

<p align="center">Excerpt of <code>timetable.task_chain</code></p>

| Column                | Type      | Definition                                                                        |
| :-------------------- | :-------- | :-------------------------------------------------------------------------------- |
| `parent_id`           | `bigint`  | TODO                                                                              |
| `task_id`             | `bigint`  | The ID of the ***base task***.                                                    |
| `run_uid`             | `text`    | The role as which the chain should be executed as.                                |
| `database_connection` | `integer` | The ID of the `timetable.database_connection` that should be used.                |
| `ignore_error`        | `boolean` | Specify if the chain should resume after encountering an error (default: `true`). |

#### 3.2.1. Chain execution configuration

Once a chain has been created, it has to be scheduled. For this, **pg_timetable** builds upon the standard **cron**-string, all the while adding multiple configuration options.

<p align="center">Excerpt of <code>timetable.chain_execution_config</code></p>
<table>
    <tr>
        <th>Column</th>
        <th>Type</th>
        <th>Definition</th>
    </tr>
    <tr>
	<td>chain_id</td>
	<td><code>bigint</code></td>
	<td>The id of the <b><i>task chain</i></b>.</td>
    </tr>
    <tr>
	<td>chain_name</td>
	<td><code>text</code></td>
	<td>The name of the <b><i>chain</i></b>.</td>
    </tr>
    <tr>
        <td><code>run_at_minute</code></td>
	<td><code>integer</code></td>
        <td rowspan="5">To achieve the <b>cron</b> equivalent of <b>*</b>, set the value to NULL.</td>
    </tr>
    <tr>
        <td><code>run_at_hour</code></td>
	<td><code>integer</code></td>
    </tr>
    <tr>
        <td><code>run_at_day</code></td>
	<td><code>integer</code></td>
    </tr>
    <tr>
        <td><code>run_at_month</code></td>
	<td><code>integer</code></td>
    </tr>
    <tr>
        <td><code>run_at_day_of_week</code></td>
	<td><code>integer</code></td>
    </tr>
    <tr>
        <td><code>max_instances</code></td>
	<td><code>integer</code></td>
	<td>The amount of instances that this chain may have running at the same time.</td>
    </tr>
    <tr>
        <td><code>live</code></td>
	<td><code>boolean</code></td>
	<td>Control if the chain may be executed once it reaches its schedule.</td>
    </tr>
    <tr>
        <td><code>self_destruct</code></td>
	<td><code>boolean</code></td>
	<td>Self destruct the chain.</td>
    </tr>
    <tr>
        <td><code>exclusive_execution</code></td>
	<td><code>boolean</code></td>
	<td>TODO</td>
    </tr>
    <tr>
        <td><code>excluded_execution_configs</code></td>
	<td><code>integer[]</code></td>
	<td>TODO</td>
    </tr>
</table>​

#### 3.2.2. Chain execution parameters

As mentioned above, base tasks are simple skeletons (e.g. *send email*, *vacuum*, etc.).
In most cases, they have to be brought to live by passing parameters to the execution.

<p align="center">Excerpt of <code>timetable.chain_execution_paramaters</code></p>

| Column                   | Type    | Definition                                       |
| :----------------------- | :------ | :----------------------------------------------- |
| `chain_execution_config` | bigint  | The ID of the chain execution configuration.     |
| `chain_id`               | bigint  | The ID of the chain.                             |
| `order_id`               | integer | The order of the parameter.                      |
| `value`                  | jsonb   | A `string` JSON array containing the paramaters. |

### 3.3. Example usages

A variety of examples can be found in the `/samples` directory.

### 3.4 Examle functions
Create a Job with the `timetable.job_add` function.

| Parameter                   | Type    | Definition                                       | Default |
| :----------------------- | :------ | :----------------------------------------------- |:---------|
| `task_name`     | text  | The name of the Task ||
| `task_function` | text  | The function wich will be executed. ||
| `task_type`     | text  | Type of the function `SQL`,`SHELL` and `BUILTIN` |SQL|
| `by_cron`       | text  | Time Schedule in Cron Syntax                      ||
| `by_minute`     | text  | This specifies the minutes on which the job is to run |ALL|
| `by_hour`       | text  | This specifies the hours on which the job is to run |ALL|
| `by_day`        | text  | This specifies the days on which the job is to run. |ALL|
| `by_month`      | text  | This specifies the month on which the job is to run |ALL|
| `by_day_of_week`| text  | This specifies the day of week (0,7 is sunday)  on which the job is to run |ALL|
| `max_instances` | integer | The amount of instances that this chain may have running at the same time. |NULL|
| `live`          | boolean | Control if the chain may be executed once it reaches its schedule. |FALSE|
| `self_destruct` | boolean | Self destruct the chain. |FALSE|

If the parameter `by_cron` is used all other `by_*` (`by_minute`,`by_hour`,`by_day`,`by_month`,`by_day_of_week`) will be ignored.

### 4 Usage

```select timetable.job_add('MyJob','Select public.my_func()','SQL','0 1 1 * *');```

Run "MyJob"

## 5. Database logging and transactions

The entire activity of **pg_timetable** is logged in database tables (`timetable.log` and `timetable.execution_log`). Since there is no need to parse files when accessing log data, the representation through an UI can be easily achieved.

Furthermore, this behavior allows a remote host to access the log in a straightforward manner, simplifying large and/or distributed applications.
>Note: Logs are written in a separate transaction, in case the chain fails.

## 6. Runtime information

In order to examine the activity of **pg_timetable**, the table `timetable.run_status` can be queried. It contains information about active jobs and their current parameters.

## 7. Schema diagram

![Schema diagram](sql/timetable_schema.png?raw=true "Schema diagram")

## 8. Contributing

If you want to contribute to **pg_timetable** and help make it better, feel free to open an [issue][issue] or even consider submitting a pull request.

[issue]: https://github.com/cybertec-postgresql/pg_timetable/issues

## 9. Support

For professional support, please contact [Cybertec][cybertec].

[cybertec]: https://www.cybertec-postgresql.com/


## 10. Authors

[Pavlo Golub](https://github.com/pashagolub) and [Hans-Jürgen Schönig](https://github.com/postgresql007).
