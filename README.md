pg_timetable: Advanced scheduling for PostgreSQL
================================================


pg_timetable is an advanced job scheduler for PostgreSQL, which has many
advantages over traditional schedulers such as cron and many others. It is
completely database driven and provides a couple of advanced concepts.

Key features
------------

The key features are:

	* Tasks can be arranged in chains
	* A chain can consist of SQL and executables
	* Parameters can be passed to chains
	* Missed tasks (maybe due to downtime) can be automatically done later
	* Support for configurable repetitons
	* Builtin tasks such as sending emails, etc.
	* Fully database driven configuration
	* Full support for database driven logging
	* Cron-style scheduling
	* Optional concurrency protection

In many cases cron is just not enough. That is exactly why we made pg_timetable.


Features and advanced functionality
-----------------------------------

As we have shown pg_timetable offers a large set of advanced features, which
exceed what you are normally used to and what you know from classical
schedulers. So, how does pg_timetable work? Here are some of the most important
concepts:

The most basic building block is a "base task", which can be

	* an SQL snippet
	* an external program
	* an internal task

An SQL snippet can be any SQL statement. Maybe you want to do some cleanup,
refresh a materialized view or simply process some data. An external program can
be anything, which can be called from the command line. Maybe it is a small
shell script, a Python program or some other program, which does something
useful for you. Internal task is something, we implemented directly in Go.
Sending emails or so is really easier to handle in a high-level programming
language and therefore we decided to do that in the Go application directly to
make things more portable and easier to use (running "mail" from command line is
not what most people are looking for - especially not on Windows or so).

The next building block is a "chain". A chain is simply a list of tasks. An
example of a chain would be:

	* Start a transaction
	* Download some files from a server
	* Import those files
	* Run some aggregations
	* Commit the transaction
	* Remove the files from disk

What you have seen is pg_timetable's ability to span transactions over more than
just one task. Than is very convenient if you are integrating database work with
the outside world (e.g. if you want to download the data from somewhere or so).
Traditionally one had to write a separate program for those kind of operations.
With pg_timetable it is simply a matter of configuration. 

But there is more: In a chain there are also a couple of more field:

	* ignore_error
	* database_connection
	* run_uid

ignore_error specifies is a chain ends after a job has failed or if it can
continue safely. Why do we care? Suppose we download some files to import them.
The download files but we still want to insert into the database that no rows
have been imported and that we at least attempted to import.

database_connection allows you to run a chain given a certain database
connection. The same scheduler might be used to import and aggregate data on
server A and perform some cleanup on server B concurrently.

Finally you can define, which user to use to execute a chain. To backup your
database running as "postgres" might be just fine. To update the operating
system some other user (root?) is what you need.

The next thing to take care of is the scheduling part: Chains can be executed
periodically or just once. Here is how it works:

	* run_at_minute
	* run_at_hour
	* run_at_day
	* run_at_month
	* run_at_day_of_week
	* max_instances
	* live (default false)
	* self_destruct (default false)
	* exclusive_execution (default false)
	* excluded_execution_configs (integer[])

As you can see the configuration part has been much taken from cron directly to
make it easier for people to configure the scheduler. Adding a NULL value to the
run\* fields basically means the same as a star in cron (= "do it always"). What
is noteworthy here is the max_instances field: You can tell pg_timetable how
often the same chain is allowed to run concurrently. In many cases only one
incarnation of a script is allowed to run (you do not want to backup the same
database multiple times at the same time - you want to skip a backup run in case
the previous run has not ended yet). This will avoid many cases where system
entered a death spital because processes kept adding up.

In addition to the ability to limit concurrent executions you can disable a
chain easily (= set "live" to false). A chain can also be marked as
"self_destruct". We basically use that to execute stuff exactly once. Why is
that important? Here is an example: Suppose a customer has not paid his receipt.
In two weeks he will receive an email that you have successfully deleted his
account. Once the account is deleted the chain is not needed anymore and it will
disable itself automatically. In case the execution time was missed because the
scheduler happened to be down, the system will try again if told to do so. In
fact "self destruct" is the pg_timetable equivalent to the UNIX "at" command.


Chains and parameters
---------------------

As mentioned before base tasks are basically skeletons (e.g. "send email" or
"count a table", etc.). When you run a chain you can pass parameter to the
execution. This allows you to reuse chains and just fire them up with different
settings. For example: 500 users might receive a "we will delete you message"
during a day and you can simple fire them up by adding an entry to a table.
pg_timetable will take care of all the rest. It is the ideal solution to handle
asynchronous execution of jobs.


Database logging and transactions
---------------------------------

pg_timetable will send all its log information to database tables (timetable.log
and timetable.execution_log). This allows you to easily create a UI on top of
your PostgreSQL database and there is no need to parse logfiles. An additional
advantage is that the log can be accessed from a remote host easily, which is
again super useful if you happen to build large or even distributed
applications. In many cases pg_timetable will control jobs on an array of
servers - not just on one box.

NOTE: Logs will be written in SEPARATE transactions in case chains fail.


Inspecting what pg_timetable does
---------------------------------

If you want to figure out what pg_timetable is doing at the moment we recommend
to check out the timetable.run_status table. It contains information about which
jobs are running at the moment and which parameters happen to be in use. It
helps you to debug your infrastructure fast and easily.

Install and run
---------------

1. You need to downlod and [install Go](https://golang.org/doc/install) in your system.
2. Download and install `pg_timetable` sources and dependent packages:
```
$ env GIT_TERMINAL_PROMPT=1 go get github.com/cybertec-postgresql/pg_timetable/
Username for 'https://github.com': cyberboy
Password for 'https://cyberboy@github.com': <cyberpwd> 
```
3. Setup PostgreSQL database and role:
```
CREATE DATABASE timetable;
CREATE USER scheduler PASSWORD 'somestrong';
GRANT CREATE ON DATABASE timetable TO scheduler;
```

4. Run `pg_timetable`:
```
$ cd ~/go/src/github.com/cybertec-postgresql/pg_timetable/
$ go run main.go --dbname=dbname --name=worker001 --user=scheduler --password=somestrong
```
or
```
$ go build
$ ./pg_timetable --dbname=dbname --name=worker001 --user=scheduler --password=somestrong
```

5. Run tests in all sub-folders of the project:
```
$ cd ~/go/src/github.com/cybertec-postgresql/pg_timetable/
$ go get github.com/stretchr/testify/
$ go test ./...
```

Patches are welcome
----------------------

If you like pg_timetable or if you want more features - we are always open to
contributions. Send us a patch and we will be glad to review and maybe include
it to make pg_timetable even better in the future.

Support
-------

Open an [issue][issue] on Gitgub if you have problems or questions.

For professional support, please contact [Cybertec][cybertec].


 [issue]: https://github.com/cybertec-postgresql/pg_timetable/issues
 [cybertec]: https://www.cybertec-postgresql.com/


Authors:
--------

Pavlo Golub and Hans-Jürgen Schönig


