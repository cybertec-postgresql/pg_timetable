from flask import Flask
from flask import escape, request, redirect, render_template
import os
import json
import datetime
import psycopg2

from env import build_env
from env import Default

cwd = os.path.dirname(os.path.realpath(__file__))

ENV_VAR_PREFIX = "PG_TIMETABLE"
ENV = build_env(
    ENV_VAR_PREFIX,
    {
        "DBNAME": Default("postgres"),
        "USER": Default("postgres"),
        "PASSWORD": Default(""),
        "HOST": Default("db"),
    },
)


app = Flask(__name__, static_url_path='/static')
#app.config['EXPLAIN_TEMPLATE_LOADING'] = True

class Model(object):
    def __init__(self):
        self.conn = psycopg2.connect(
            "dbname={dbname} user={user} password={password} host={host}".format(
                dbname=ENV.dbname, user=ENV.user, password=ENV.password, host=ENV.host
            )
        )
        self.cur = self.conn.cursor()


class Task(Model):
    def __init__(self, task_id=None, task_name=None, task_function=None, task_kind='SQL'):
        self.task_id=task_id
        self.task_name=task_name
        self.task_function=task_function
        self.task_kind=task_kind
        super().__init__()


    def get_all(self):
        self.cur.execute("SELECT task_id, name, kind, script FROM timetable.base_task")
        records = self.cur.fetchall()
        result = []
        for row in records:
            result.append(
                 {"task_id": row[0],
                  "task_name": row[1],
                  "task_kind": row[2] or "",
                  "task_function": row[3]
                }
            )

        return result


    def get_by_id(self, task_id):
        self.cur.execute(
            "SELECT name, kind, script FROM timetable.base_task where task_id = %s", (task_id,))
        records = self.cur.fetchall()
        row = records[0]
        result = {"task_id": task_id,
                  "task_name": row[0],
                  "task_kind": row[1] or "",
                  "task_function": row[2]
                }
        return result

    def save(self):
        if self.task_id is None:
            self.cur.execute(
                "INSERT INTO timetable.base_task (name, kind, script) VALUES (%s, %s, %s)", (self.task_name, self.task_kind, self.task_function)
            )
        else:
            self.cur.execute(
                "UPDATE timetable.base_task set name = %s, kind = %s, script = %s where task_id = %s", (self.task_name, self.task_kind, self.task_function, self.task_id))
        self.conn.commit()

class ChainExecutionParameters(Model):
    def __init__(self, chain_execution_config=None, chain_id=None, order_id=None, value=None):
        self.chain_execution_config = chain_execution_config
        self.chain_id = chain_id
        self.order_id = order_id
        self.value = value
        super().__init__()


    def get_all(self):
        self.cur.execute(
            "SELECT chain_execution_config, chain_id, order_id, value FROM timetable.chain_execution_parameters"
        )
        records = self.cur.fetchall()
        result = []
        for row in records:
            result.append(
                {
                    "chain_execution_config": row[0],
                    "chain_id": row[1],
                    "order_id": row[2],
                    "value": row[3],
                }
            )
        return result

    def save(self):
        self.cur.execute(
                "insert into timetable.chain_execution_parameters (chain_execution_config, chain_id, order_id, value) VALUES (%s, %s, %s, %s) ON CONFLICT (chain_execution_config, chain_id, order_id) DO UPDATE set value = %s", (self.chain_execution_config, self.chain_id, self.order_id, self.value, self.value))
        self.conn.commit()

    def delete(self):
        self.cur.execute(
                "delete from timetable.chain_execution_parameters where chain_execution_config = %s and chain_id = %s and order_id = %s", (self.chain_execution_config, self.chain_id, self.order_id))
        self.conn.commit()


    def get_by_id(self, chain_execution_config, chain_id, order_id):
        self.cur.execute(
            "SELECT chain_execution_config, chain_id, order_id, value FROM timetable.chain_execution_parameters where chain_execution_config = %s and chain_id = %s and order_id = %s", (chain_execution_config, chain_id, order_id)
        )
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = {
            "chain_execution_config": row[0],
            "chain_id": row[1],
            "order_id": row[2],
            "value": row[3],
        }
        return result


class ChainExecutionConfig(Model):
    def __init__(self, chain_execution_config=None, chain_id=None, chain_name=None, run_at_minute=None, run_at_hour=None, run_at_day=None, run_at_month=None, run_at_day_of_week=None, max_instances=None, live=None, self_destruct=None, exclusive_execution=None, excluded_execution_configs=None, client_name=None, task_id=None):
        self.chain_execution_config = chain_execution_config
        self.chain_id = chain_id
        self.chain_name = chain_name
        self.run_at_minute = run_at_minute
        self.run_at_hour = run_at_hour
        self.run_at_day = run_at_day
        self.run_at_month = run_at_month
        self.run_at_day_of_week = run_at_day_of_week
        self.max_instances = max_instances
        self.live = live
        self.self_destruct = self_destruct
        self.exclusive_execution = exclusive_execution
        self.excluded_execution_configs = excluded_execution_configs
        self.client_name = client_name
        self.task_id = task_id
        self.parents_done = []
        super().__init__()


    def get_all(self):
        self.cur.execute(
            "SELECT chain_execution_config, chain_id, chain_name, run_at_minute, run_at_hour, run_at_day, run_at_month, run_at_day_of_week, max_instances, live, self_destruct, exclusive_execution, excluded_execution_configs, client_name FROM timetable.chain_execution_config"
        )
        records = self.cur.fetchall()
        result = []
        for row in records:
            result.append(
                {
                    "chain_execution_config": row[0],
                    "chain_id": row[1],
                    "chain_name": row[2],
                    "run_at_minute": row[3],
                    "run_at_hour": row[4],
                    "run_at_day": row[5],
                    "run_at_month": row[6],
                    "run_at_day_of_week": row[7],
                    "max_instances": row[8],
                    "live": row[9],
                    "self_destruct": row[10],
                    "exclusive_execution": row[11],
                    "excluded_execution_configs": row[12],
                    "client_name": row[13]
                }
            )
        return result

    def save(self):
        if self.chain_execution_config is None and self.chain_id is None and self.task_id is not None:
            self.cur.execute(
                "WITH ins AS (INSERT INTO timetable.task_chain (parent_id, task_id, run_uid, database_connection, ignore_error) VALUES (DEFAULT, %s, DEFAULT, DEFAULT, DEFAULT) RETURNING chain_id) INSERT INTO timetable.chain_execution_config (chain_name, chain_id, run_at_minute, run_at_hour, run_at_day, run_at_month, run_at_day_of_week, max_instances, live, self_destruct, exclusive_execution, excluded_execution_configs, client_name) SELECT %s, chain_id, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s from ins", (self.task_id, self.chain_name, self.run_at_minute, self.run_at_hour, self.run_at_day, self.run_at_month, self.run_at_day_of_week, self.max_instances, self.live, self.self_destruct, self.exclusive_execution, self.excluded_execution_configs, self.client_name))
        elif self.chain_execution_config is None:
            self.cur.execute(
                "INSERT INTO timetable.chain_execution_config (chain_name, chain_id, run_at_minute, run_at_hour, run_at_day, run_at_month, run_at_day_of_week, max_instances, live, self_destruct, exclusive_execution, excluded_execution_configs, client_name) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)", (self.chain_name, self.chain_id, self.run_at_minute, self.run_at_hour, self.run_at_day, self.run_at_month, self.run_at_day_of_week, self.max_instances, self.live, self.self_destruct, self.exclusive_execution, self.excluded_execution_configs, self.client_name))
        else:
            self.cur.execute(
                "UPDATE timetable.chain_execution_config SET chain_execution_config = %s, chain_id = %s, chain_name = %s, run_at_minute = %s, run_at_hour = %s, run_at_day = %s, run_at_month = %s, run_at_day_of_week = %s, max_instances = %s, live = %s, self_destruct = %s, exclusive_execution = %s, excluded_execution_configs = %s, client_name = %s where chain_execution_config = %s", (self.chain_execution_config, self.chain_id, self.chain_name, self.run_at_minute, self.run_at_hour, self.run_at_day, self.run_at_month, self.run_at_day_of_week, self.max_instances, self.live, self.self_destruct, self.exclusive_execution, self.excluded_execution_configs, self.client_name, self.chain_execution_config))
        self.conn.commit()


    def get_by_id(self, id):
        self.cur.execute(
            "SELECT chain_execution_config, chain_id, chain_name, run_at_minute, run_at_hour, run_at_day, run_at_month, run_at_day_of_week, max_instances, live, self_destruct, exclusive_execution, excluded_execution_configs, client_name FROM timetable.chain_execution_config where chain_execution_config = %s", (id,)
        )
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = {
            "chain_execution_config": row[0],
            "chain_id": row[1],
            "chain_name": row[2],
            "run_at_minute": row[3],
            "run_at_hour": row[4],
            "run_at_day": row[5],
            "run_at_month": row[6],
            "run_at_day_of_week": row[7],
            "max_instances": row[8],
            "live": row[9],
            "self_destruct": row[10],
            "exclusive_execution": row[11],
            "excluded_execution_configs": row[12],
            "client_name": row[13],
            "chain": self.get_chain_by_id(row[1])
        }
        return result

    def get_chain_by_id(self, chain_id):
        self.cur.execute("SELECT chain_id, parent_id, task_id, run_uid, database_connection, ignore_error FROM timetable.task_chain where chain_id = %s", (chain_id,))
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = {
                "chain_id": row[0],
                "parent_id": row[1],
                "run_uid": row[3],
                "database_connection": row[4],
                "ignore_error": row[5],
                "next": self.get_chain_by_parent(parent_id=row[0]),
                "parameters": self.get_chain_parameters(chain_id=row[0]),
                "task": self.get_task(task_id=row[2])
                }
        return result

    def get_task(self, task_id):
        self.cur.execute(
            "SELECT name, kind, script FROM timetable.base_task where task_id = %s", (task_id,))
        records = self.cur.fetchall()
        row = records[0]
        result = {"task_id": task_id,
                  "task_name": row[0],
                  "task_kind": row[1] or "",
                  "task_function": row[2]
                }
        return result


    def get_chain_by_parent(self, parent_id):
        if parent_id not in self.parents_done:
            self.parents_done.append(parent_id)
        else:
            return {}
        self.cur.execute("SELECT chain_id, parent_id, task_id, run_uid, database_connection, ignore_error FROM timetable.task_chain where parent_id = %s", (parent_id,))
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = {
                "chain_id": row[0],
                "parent_id": row[1],
                "run_uid": row[3],
                "database_connection": row[4],
                "ignore_error": row[5],
                "next": self.get_chain_by_parent(parent_id=row[0]),
                "parameters": self.get_chain_parameters(chain_id=row[0]),
                "task": self.get_task(task_id=row[2])
                }
        return result


    def get_chain_parameters(self, chain_id):
        self.cur.execute(
            "SELECT order_id, value FROM timetable.chain_execution_parameters where chain_id = %s and chain_execution_config = %s order by order_id", (chain_id, self.chain_execution_config)
        )
        records = self.cur.fetchall()
        result = []
        for row in records:
            result.append(
                {
                    "chain_execution_config": self.chain_execution_config,
                    "chain_id": chain_id,
                    "order_id": row[0],
                    "value": row[1],
                }
            )
        return result

class Chain(Model):
    def __init__(self, chain_id=None, task_id=None, parent_id=None, run_uid=None, database_connection=None, ignore_error=None):
        self.chain_id=chain_id
        self.task_id=task_id
        self.parent_id=parent_id
        self.run_uid=run_uid
        self.database_connection=database_connection
        self.ignore_error=ignore_error
        self.parents_done = []
        super().__init__()


    def get_all(self, only_base=True):
        if only_base:
            self.cur.execute("SELECT chain_id, task_id, run_uid, database_connection, ignore_error FROM timetable.task_chain where parent_id is null")
        else:
            self.cur.execute("SELECT chain_id, task_id, run_uid, database_connection, ignore_error FROM timetable.task_chain")
        records = self.cur.fetchall()
        result = []
        for row in records:
            result.append(
                {
                    "chain_id": row[0],
                    "task_id": row[1],
                    "run_uid": row[2],
                    "database_connection": row[3],
                    "ignore_error": row[4],
                }
            )

        return result

    def notparents(self):
        self.cur.execute("select a.chain_id, a.parent_id, a.task_id, a.run_uid, a.database_connection, a.ignore_error FROM timetable.task_chain a left outer join timetable.task_chain b on a.chain_id = b.parent_id where b.parent_id is null")
        records = self.cur.fetchall()
        result = []
        for row in records:
            result.append(
                {
                    "chain_id": row[0],
                    "parent_id": row[1],
                    "task_id": row[2],
                    "run_uid": row[3],
                    "database_connection": row[4],
                    "ignore_error": row[5],
                }
            )

        return result


    def get_by_id(self, chain_id):
        self.cur.execute("SELECT chain_id, parent_id, task_id, run_uid, database_connection, ignore_error FROM timetable.task_chain where chain_id = %s", (chain_id,))
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = {
                "chain_id": row[0],
                "parent_id": row[1],
                "task_id": row[2],
                "run_uid": row[3],
                "database_connection": row[4],
                "ignore_error": row[5],
                "next": self.get_by_parent(parent_id=row[0])
                }
        return result

    def get_by_parent(self, parent_id):
        if parent_id not in self.parents_done:
            self.parents_done.append(parent_id)
        else:
            return {}
        self.cur.execute("SELECT chain_id, parent_id, task_id, run_uid, database_connection, ignore_error FROM timetable.task_chain where parent_id = %s", (parent_id,))
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = {
                "chain_id": row[0],
                "parent_id": row[1],
                "task_id": row[2],
                "run_uid": row[3],
                "database_connection": row[4],
                "ignore_error": row[5],
                "next": self.get_by_parent(parent_id=row[0])
                }
        return result

    def save(self):
        if self.chain_id is None:
            self.cur.execute(
                "INSERT INTO timetable.task_chain (parent_id, task_id, run_uid, database_connection, ignore_error) VALUES (%s, %s, %s, %s, %s)", (self.parent_id, self.task_id, self.run_uid, self.database_connection, self.ignore_error)
            )
        else:
            self.cur.execute(
                "UPDATE timetable.task_chain set parent_id = %s, task_id = %s, run_uid = %s, database_connection = %s, ignore_error = %s where chain_id = %s", (self.parent_id, self.task_id, self.run_uid, self.database_connection, self.ignore_error, self.chain_id,))
        self.conn.commit()

    def delete(self):
        self.cur.execute(
                "delete from timetable.task_chain where chain_id = %s", (self.chain_id,))
        self.conn.commit()

class ExecutionLog(Model):

    def get_by_chain_config(self, chain_execution_config):
        self.cur.execute("SELECT chain_execution_config, chain_id, task_id, name, script,  kind, last_run, finished, returncode, pid FROM timetable.execution_log where chain_execution_config = %s", (chain_execution_config,))
        records = self.cur.fetchall()
        if len(records) == 0:
            return {}
        row = records[0]
        result = []
        for row in records:
            result.append(
                    {
                "chain_execution_config": row[0],
                "chain_id": row[1],
                "task_id": row[2],
                "name": row[3],
                "script": row[4],
                "kind": row[5],
                "last_run": row[6],
                "finished": row[7],
                "returncode": row[8],
                "pid": row[9],
                })
        return result

def validate_string(s):
    if isinstance(s, str) and len(s) == 0:
        return None
    return s
def validate_bool(b):
    return True if b else False

@app.route('/')
def index():
    return redirect(f"/chain_execution_config/", code=302)

@app.route('/tasks/add/', methods=["GET", "POST"])
def add_base_task():
    if request.method == 'GET':
        return render_template("create_task.html", task=Task())
    else:
        task_name = validate_string(request.form.get('task_name'))
        task_function = validate_string(request.form.get('task_function'))
        task_kind = validate_string(request.form.get('task_kind'))
        Task(None, task_name, task_function, task_kind).save()
        return redirect("/tasks/", code=302)

@app.route('/task/<int:task_id>/')
def view_task(task_id):
    return render_template("view_task.html", obj=Task().get_by_id(task_id))

@app.route('/task/<int:task_id>/edit/', methods=["GET", "POST"])
def edit_task(task_id):
    if request.method == 'GET':
        return render_template("edit_task.html", obj=Task().get_by_id(task_id))
    else:
        task_name = validate_string(request.form.get('task_name'))
        task_function = validate_string(request.form.get('task_function'))
        task_kind = validate_string(request.form.get('task_kind'))
        Task(task_id, task_name, task_function, None, task_kind).save()
        return redirect(f"/task/{task_id}", code=302)

@app.route('/chain/<int:chain_id>/')
def view_chain(chain_id):
    return render_template("view_chain.html", obj=Chain().get_by_id(chain_id))

@app.route('/chain/<int:chain_id>/edit/', methods=["GET", "POST"])
def edit_chain(chain_id):
    if request.method == 'GET':
        return render_template("edit_chain.html", obj=Chain().get_by_id(chain_id), chains=Chain().notparents(), tasks=Task().get_all())
    else:
        task_id = validate_string(request.form.get('task_id'))
        parent_id = validate_string(request.form.get('parent_id'))
        run_uid = validate_string(request.form.get('run_uid'))
        database_connection = validate_string(request.form.get('database_connection'))
        ignore_error = validate_bool(request.form.get('ignore_error'))
        Chain(chain_id, task_id, parent_id, run_uid, database_connection, ignore_error).save()
        return redirect(f"/chain_execution_config/", code=302)

@app.route('/chain/<int:parent_id>/add/', methods=["GET", "POST"])
def add_chain_to_parent(parent_id):
    if request.method == 'GET':
        return render_template("edit_chain.html", obj=Chain(None, parent_id), tasks=Task().get_all())
    else:
        task_id = validate_string(request.form.get('task_id'))
        run_uid = validate_string(request.form.get('run_uid'))
        database_connection = validate_string(request.form.get('database_connection'))
        ignore_error = validate_bool(request.form.get('ignore_error'))
        if parent_id == 0:
            parent_id = None
        Chain(None, task_id, parent_id, run_uid, database_connection, ignore_error).save()
        return redirect(f"/chain_execution_config/", code=302)

@app.route('/chain/<int:chain_id>/delete/', methods=["GET", "POST"])
def delete_chain(chain_id):
    if request.method == 'GET':
        return render_template("delete.html", obj=Chain().get_by_id(chain_id))
    else:
        Chain(chain_id).delete()
        return redirect(f"/chain_execution_config/", code=302)


@app.route('/chain_execution_config/add/', methods=["GET", "POST"])
def add_chain_execution_configs():
    if request.method == 'GET':
        return render_template("edit_chain_execution_config.html", obj=ChainExecutionConfig(), chains=Chain().get_all(), tasks=Task().get_all())
    else:
        chain_id = validate_string(request.form.get('chain_id'))
        task_id = validate_string(request.form.get('task_id'))
        chain_name = validate_string(request.form.get('chain_name'))
        run_at_minute = validate_string(request.form.get('run_at_minute'))
        run_at_hour = validate_string(request.form.get('run_at_hour'))
        run_at_day = validate_string(request.form.get('run_at_day'))
        run_at_month = validate_string(request.form.get('run_at_month'))
        run_at_day_of_week = validate_string(request.form.get('run_at_day_of_week'))
        max_instances = validate_string(request.form.get('max_instances'))
        live = validate_bool(request.form.get('live'))
        self_destruct = validate_bool(request.form.get('self_destruct'))
        exclusive_execution = validate_bool(request.form.get('exclusive_execution'))
        excluded_execution_configs = validate_string(request.form.get('excluded_execution_configs'))
        client_name = validate_string(request.form.get('client_name'))
        ChainExecutionConfig(None, chain_id, chain_name, run_at_minute, run_at_hour, run_at_day, run_at_month, run_at_day_of_week, max_instances, live, self_destruct, exclusive_execution, excluded_execution_configs, client_name, task_id).save()
        return redirect(f"/chain_execution_config/", code=302)

@app.route('/chain_execution_config/')
def list_chain_execution_configs():
    return render_template("list_chain_execution_configs.html", list=ChainExecutionConfig().get_all())

@app.route('/chain_execution_config/<int:id>/')
def view_chain_execution_configs(id):
    return render_template("view_chain_execution_config.html", obj=ChainExecutionConfig(id).get_by_id(id))

@app.route('/chain_execution_config/<int:id>/edit/', methods=["GET", "POST"])
def edit_chain_execution_configs(id):
    if request.method == 'GET':
        return render_template("edit_chain_execution_config.html", obj=ChainExecutionConfig().get_by_id(id), chains=Chain().get_all())
    else:
        chain_id = validate_string(request.form.get('chain_id'))
        chain_name = validate_string(request.form.get('chain_name'))
        run_at_minute = validate_string(request.form.get('run_at_minute'))
        run_at_hour = validate_string(request.form.get('run_at_hour'))
        run_at_day = validate_string(request.form.get('run_at_day'))
        run_at_month = validate_string(request.form.get('run_at_month'))
        run_at_day_of_week = validate_string(request.form.get('run_at_day_of_week'))
        max_instances = validate_string(request.form.get('max_instances'))
        live = validate_bool(request.form.get('live'))
        self_destruct = validate_bool(request.form.get('self_destruct'))
        exclusive_execution = validate_bool(request.form.get('exclusive_execution'))
        excluded_execution_configs = validate_string(request.form.get('excluded_execution_configs'))
        client_name = validate_string(request.form.get('client_name'))
        ChainExecutionConfig(id, chain_id, chain_name, run_at_minute, run_at_hour, run_at_day, run_at_month, run_at_day_of_week, max_instances, live, self_destruct, exclusive_execution, excluded_execution_configs, client_name).save()
        return redirect(f"/chain_execution_config/{id}/", code=302)


@app.route('/chain_execution_parameters/<int:chain_execution_config>/<int:chain_id>/<int:order_id>/add/', methods=["GET", "POST"])
def create_chain_execution_parameters(chain_execution_config, chain_id, order_id):
    if request.method == 'GET':
        return render_template("edit_chain_execution_parameters.html", obj=ChainExecutionParameters(chain_execution_config, chain_id, order_id))
    else:
        order = validate_string(request.form.get('order_id'))
        value = validate_string(request.form.get('value'))
        ChainExecutionParameters(chain_execution_config, chain_id, order, value).save()
        return redirect(f"/chain_execution_parameters/{chain_execution_config}/{chain_id}/{order}/", code=302)

@app.route('/chain_execution_parameters/<int:chain_execution_config>/<int:chain_id>/<int:order_id>/')
def view_chain_execution_parameters(chain_execution_config, chain_id, order_id):
    return render_template("view_chain_execution_parameters.html", obj=ChainExecutionParameters().get_by_id(chain_execution_config, chain_id, order_id))

@app.route('/chain_execution_parameters/<int:chain_execution_config>/<int:chain_id>/<int:order_id>/edit/', methods=["GET", "POST"])
def edit_chain_execution_parameters(chain_execution_config, chain_id, order_id):
    if request.method == 'GET':
        return render_template("edit_chain_execution_parameters.html", obj=ChainExecutionParameters().get_by_id(chain_execution_config, chain_id, order_id))
    else:
        order = validate_string(request.form.get('order_id'))
        value = validate_string(request.form.get('value'))
        ChainExecutionParameters(chain_execution_config, chain_id, order, value).save()
        return redirect(f"/chain_execution_parameters/{chain_execution_config}/{chain_id}/{order}/", code=302)

@app.route('/execution_log/<int:id>/')
def view_execution_logs(id):
    return render_template("view_execution_logs.html", list=ExecutionLog().get_by_chain_config(id))

