Getting started
================================================================

A variety of examples can be found in the :doc:`samples`.

In a real world usually it's enough to use simple jobs. Under this term we understand:

* job is a chain with only one **task** (step) in it;
* it doesn't use complicated logic, but rather simple **command**;
* it doesn't require complex transaction handling, since one task is implicitely executed as a single transaction.

For such a group of chains we've introduced a special function `timetable.add_job`.

.. function:: add_job()

    Creates a simple one-task chain

    :param job_name: The unique name of the **chain** and **command**.
    :type job_name: text

    :param job_schedule: Time schedule in сron syntax.
    :type job_schedule: timetable.cron

    :param job_command: The SQL which will be executed.
    :type job_command: text

    :param job_client_name: Specifies which client should execute the chain. Set this to `NULL` to allow any client.
    :type job_client_name: text

    :param job_type: Type of the function `SQL`,`PROGRAM` and `BUILTIN`.
    :type job_type: text

    :param job_max_instances: The amount of instances that this chain may have running at the same time.
    :type job_max_instances: integer

    :param job_live: Control if the chain may be executed once it reaches its schedule.
    :type job_live: boolean

    :param job_self_destruct: Self destruct the chain after execution.
    :type job_self_destruct: boolean

    :param job_ignore_errors: Ignore error during execution.
    :type job_ignore_errors: boolean

    :returns: the chain id
    :rtype: int

