REST API
================================================

**pg_timetable** has a rich REST API, which can be used by external tools in order to perform start/stop/reinitialize/restarts/reloads, 
by any kind of tools to perform HTTP health checks, and of course, could also be used for monitoring.

Below you will find the list of **pg_timetable** REST API endpoints.

Health check endpoints
------------------------------------------------

``GET /liveness`` 
    Always returns HTTP status code ``200``, indicating that **pg_timetable** is running.

``GET /readiness``
    Returns HTTP status code ``200`` when the **pg_timetable** is running, and the scheduler is in the main loop processing chains. 
    If the scheduler connects to the database, creates the database schema, or upgrades it, it will return the HTTP status code ``503``.

Chain management endpoints
------------------------------------------------

``GET /startchain?id=<chain-id>`` 
    Returns HTTP status code ``200`` if the chain with the given id can be added to the worker queue. It doesn't, however, mean the chain execution starts immediately. It is up to the worker to perform load and other checks before starting the chain.
    In the case of an error, the HTTP status code ``400`` followed by an error message returned.

``GET /stopchain?id=<chain-id>`` 
    Returns HTTP status code ``200`` if the chain with the given id is working at the moment and can be stopped. If the chain is running the  
    cancel signal would be sent immediately.
    In the case of an error, the HTTP status code ``400`` followed by an error message returned. 