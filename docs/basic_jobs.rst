Getting started with simplest jobs!
================================================================

A variety of examples can be found in the :doc:`samples`.

In a real world usually it's enough to use simple jobs. Under this term we understand:

* job is a chain with only one **task** (step) in it;
* it doesn't use complicated logic, but rather simple **command**;
* it doesn't require complex transaction handling, since one task is implicitely executed as a single transaction.

For such a group of chains we've introduced a special function `timetable.add_job`.

.. function:: add_job()

   Creates a simple one-task chain

    :param job_name text: f
    :param job_schedule timetable.cron: f
    :param job_command text: f
    :param job_client_name text: f
    :param job_type text: f
    :param job_max_instances integer: f
    :param job_live boolean: f
    :param job_self_destruct boolean: f
    :param job_ignore_errors boolean: f
    :return: the chain id
    :rtype: int

