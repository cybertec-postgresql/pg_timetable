This is a flask application to manage pg_timetable from web inteface.
To run it you need to install requirements:

    pip install -r requirements.txt

define required variables

    export FLASK_APP=server.py
    export PG_TIMETABLE_DBNAME=postgres
    export PG_TIMETABLE_USER=postgres
    export PG_TIMETABLE_HOST=127.0.0.1
    export PG_TIMETABLE_PASSWORD=password

and start the service

    flusk run

Open http://127.0.0.1:5000 in your favorite browser.

Create base task on page http://127.0.0.1:5000/tasks/add/

Create chain ececution config on page http://127.0.0.1:5000/chain_execution_config/add/

On page http://127.0.0.1:5000/chain_execution_config/ you can see the config and you will see the links to edit it.






