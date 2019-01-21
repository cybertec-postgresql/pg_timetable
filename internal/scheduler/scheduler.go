package scheduler

import (
	"fmt"
	"os/exec"

	"github.com/besser/cron"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

//Run executes jobs
func Run() {
  var mainloopExit = false
  var pid_t[] int
  var max_pids = 0
  var n_pids = 0
  var doStartUpTasks bool = true;

	/* cleanup potential database leftovers */
	pgengine.FixSchedulerCrash()
  /* loop forever or until we ask it to stop */
  for !mainloopExit {
    /* reconnect in case of failure */
    reconnect_if_necessary(conn, connect_string);

    /* detect voip_config::disable_reports */
    res = PQexec(conn, "SELECT val FROM voip_config WHERE key = 'disable_reports'");
    if (PQresultStatus(res) != PGRES_TUPLES_OK)
    {
      /* no such table? this is a fatal error but let's just continue */
      execute_reports = 1;
    }
    else
    {
      if (PQntuples(res) == 0)
      {
        /* no such record in voip_config? execute reports */
        execute_reports = 1;
      }
      else
      {
        char *result;

        /* value is NULL? execute reports */
        if (PQgetisnull(res, 0, 0))
          execute_reports = 1;
        else
        {
          result = PQgetvalue(res, 0, 0);

          /*
           * disable_reports == true/TRUE/on/ON or any non-zero number:
           */
          if (strcasecmp(result, "on") == 0 ||
              strcasecmp(result, "true") == 0 ||
              atoi(result))
            execute_reports = 0;
          else
            execute_reports = 1;
        }
      }
    }
    PQclear(res);

    if (doStartUpTasks)
    {
      /* This is the first task execution after startup, so we will execute one-time tasks... */
      logging(LOG_DEBUG, "checking for startup task chains ...");

      asprintf(&query,
	  " SELECT   chain_execution_config, chain_id, chain_name, "
	  "    self_destruct, exclusive_execution, excluded_execution_configs, "
	  "    CASE WHEN max_instances IS NULL THEN 999 ELSE max_instances END, "
	  "     task_type "
	  " FROM   scheduler.chain_execution_config "
	  " WHERE live = 't' "
	  "  AND  task_type = 'S' " );
	  
      /* Make sure we do not execute startup tasks again */
      doStartUpTasks = 0;
    }
    else
    {
      /* ask the database which chains it has to perform */
      logging(LOG_DEBUG, "checking for task chains ...");

      asprintf(&query,
	  " SELECT   chain_execution_config, chain_id, chain_name, "
	  "    self_destruct, exclusive_execution, excluded_execution_configs, "
	  "    CASE WHEN max_instances IS NULL THEN 999 ELSE max_instances END, "
	  "    task_type "
	  " FROM   scheduler.chain_execution_config "
	  " WHERE live = 't' AND (task_type <> 'S' OR task_type IS NULL) "
	  "  AND  scheduler.check_task(chain_execution_config) = 't' " );
    }
    
    res = PQexec(conn, query);
    if   (PQresultStatus(res) != PGRES_TUPLES_OK)
    {
      logging(LOG_DEBUG, "could not query pending tasks: %s", PQerrorMessage(conn));
      PQclear(res);
      free(query);
      return;
    }

    if (PQnfields(res) != 7)
    {
      logging(LOG_DEBUG, "unexpected number of fields in query result");
      PQclear(res);
      free(query);
      return;
    }

    logging(LOG_DEBUG, "nr of chain head tuples %d", PQntuples(res));

    /* now we can loop through so chains */
    for  (i = 0; i < PQntuples(res); i++)
    {
      pid_t  pid;
      int  j, exec_config, chain_id, max_inst;
      char *params[8];

      for (j = 0; j < 8; j++)
      {
        int  isnull;

        params[j] = PQgetvalue(res, i, j);
        isnull =  PQgetisnull(res, i, j);

        if (params[j] == NULL)
        {
          logging(LOG_DEBUG, "params[%d] == NULL patched as empty string", j);
          params[j] = "";
        }
        if (isnull && (j == 0 || j == 1 || j == 2 || j == 3 || j == 6))
        {
          logging(LOG_DEBUG, "run_scheduler params[%d]='%s' IS NULL, not executing", j, params[j]);
          goto out;
        }
        logging(LOG_DEBUG, "run_scheduler params[%d]='%s'%s", j, params[j], (params[j][0] == '\0' && isnull ? " NULL" : ""));
      }

      exec_config = atoi(params[0]);
      chain_id = atoi(params[1]);
      max_inst = atoi(params[6]);
      logging(LOG_DEBUG, "calling process chain for: %d, chain_id=%d name='%s', self_destruct='%s', max_inst=%d",
          exec_config, chain_id, params[2], params[3], max_inst);

      /* Execute reports if instructed and execute any other task types. */
      if ((strcmp(params[7], "R") == 0 && execute_reports) ||
           strcmp(params[7], "R") != 0 )
      {
        /* for each chain we have to fork off a process */
        pid = process_chain(connect_string, exec_config,   /* chain_execution_config */
            chain_id,         /* chain_id */
            params[2],        /* chain_name */
            params[3],        /* self_destruct */
            max_inst);        /* max_instances */

        if (pid > 0)
          pidarray = add_pid(pidarray, pid, &n_pids, &max_pids);
      }
    }
out:

    /* free memory to avoid leaks */
    PQclear(res);
    free(query);

    /* wait for the next full minute to show up */
    wait_for_full_minute(pidarray, &n_pids);
  }

  PQfinish(conn);
}

/* ------------------------------------------
------------------------------------------
 ------------------------------------------
  ------------------------------------------
   ------------------------------------------
    ------------------------------------------ */

// RunBaseBackup runs pg_basebackup for host specified
func RunBaseBackup(backupedhost pgengine.BackupedHost) {

	pgengine.LogToDB(backupedhost.Id, "LOG", "Starting base backup for host: ", backupedhost.Id)

	command := exec.Command(baseBackupExec, fmt.Sprintf("--pgdata=%s/%d", backupDir, backupedhost.Id),
		"--format=tar", "--gzip",
		"-d", fmt.Sprintf("host='%s' port='%s' user='%s' password='%s'", backupedhost.Host, backupedhost.Port,
			backupedhost.User, backupedhost.Password))

	//TODO: take control over stdin, stdout and stderr, e.g. for file upload start, logging etc.
	//
	err := command.Run()
	if err != nil {
		pgengine.LogToDB(backupedhost.Id, "ERROR", err)
	} else {
		pgengine.LogToDB(backupedhost.Id, "LOG", "Base backup finished for host: ", backupedhost.Id)
	}
	return
}

type Dispatcher struct {
	WalReceivers  map[int]WalReceiver
	BaseBackupers map[int]BaseBackuper
	cronAgent     *cron.Cron
}

func (bd *Dispatcher) IsBaseBackupScheduled(backupedhost pgengine.BackupedHost) bool {
	if bb, ok := bd.BaseBackupers[backupedhost.Id]; ok {
		return bb.HostOpts == backupedhost && bb.CronEntryId > 0
	}
	return false
}

func (bd *Dispatcher) ScheduleBaseBackup(backupedhost pgengine.BackupedHost) (eid cron.EntryID, err error) {
	eid, err = bd.cronAgent.AddFunc(backupedhost.CronSchedule, func() { RunBaseBackup(backupedhost) })
	if err == nil {
		bd.BaseBackupers[backupedhost.Id] = BaseBackuper{CronEntryId: eid, HostOpts: backupedhost}
	}
	return
}

func (bd *Dispatcher) UnscheduleBaseBackup(bbid cron.EntryID) {
	bd.cronAgent.Remove(bbid)
	return
}

func (bd *Dispatcher) IsWalReceiverRunning(backupedhost pgengine.BackupedHost) bool {
	if wr, ok := bd.WalReceivers[backupedhost.Id]; ok {
		return wr.HostOpts == backupedhost
	}
	return false
}

func (bd *Dispatcher) RunWalReceiver(backupedhost pgengine.BackupedHost) (WR WalReceiver, err error) {

	WR.Command = exec.Command(walExec, "-D", fmt.Sprintf("%s/%d", backupDir, backupedhost.Id),
		"-S", backupedhost.Slotname,
		"-d", fmt.Sprintf("host='%s' port='%s' user='%s' password='%s'", backupedhost.Host, backupedhost.Port, backupedhost.User, backupedhost.Password))
	WR.HostOpts = backupedhost

	//TODO: take control over stdin, stdout and stderr, e.g. for file upload start, logging etc.
	//

	err = WR.Command.Start()
	if err == nil {
		bd.WalReceivers[backupedhost.Id] = WR
	}
	return
}

func (bd *Dispatcher) StopWalReceiver(backupedhost pgengine.BackupedHost) error {
	return bd.StopWalReceiverById(backupedhost.Id)
}

func (bd *Dispatcher) StopWalReceiverById(hid int) (err error) {
	if wr, ok := bd.WalReceivers[hid]; ok {
		err = wr.Command.Process.Kill()
		pgengine.LogToDB(hid, "LOG", fmt.Sprintf("Kill signal sent to wal receiver with PID: %d", wr.Command.Process.Pid))
		delete(bd.WalReceivers, hid)
	}
	return
}

func (bd *Dispatcher) StopAllWalReceivers() (err error) {
	for _, wr := range bd.WalReceivers {
		err = wr.Command.Process.Kill()
		pgengine.LogToDB(wr.HostOpts.Id, "LOG", fmt.Sprintf("Kill signal sent to wal receiver with PID: %d", wr.Command.Process.Pid))
	}
	bd.WalReceivers = make(map[int]WalReceiver)
	return
}

func (bd *Dispatcher) Finalize() {
	bd.StopAllWalReceivers()
	bd.cronAgent.Stop()
	return
}

func NewDispatcher() *Dispatcher {
	var d Dispatcher
	d.cronAgent = cron.New()
	d.cronAgent.Start()
	d.WalReceivers = make(map[int]WalReceiver)
	d.BaseBackupers = make(map[int]BaseBackuper)
	return &d
}

func init() {
	checkExeExists(walExec, "WAL receiver executable not found!")
	checkExeExists(baseBackupExec, "Base backup executable not found!")
}
