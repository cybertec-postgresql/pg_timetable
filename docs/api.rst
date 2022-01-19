REST API
================================================

**pg_timetable** has a rich REST API, which can be used by external tools in order to perform start/stop/reinitialize/restarts/reloads, 
by any kind of tools to perform HTTP health checks, and of course, could also be used for monitoring.

Below you will find the list of **pg_timetable** REST API endpoints.

Health check endpoints
------------------------------------------------

Currently, there are two health check endpoints available:

``GET /liveness`` 
    Always returns HTTP status code ``200`` what only indicates that **pg_timetable** is running.

``GET /readiness``
    Returns HTTP status code ``200`` when the **pg_timetable** is running and the scheduler is in the main loop processing chains. 
    If the scheduler connects to the database, creates the database schema, or upgrades it, it will return HTTP status code ``503``.