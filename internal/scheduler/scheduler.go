package scheduler

import (
  "fmt"
  "time"

  "github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
  "github.com/jmoiron/sqlx"
)

const workersNumber = 16

// ChainElementExecution structure describes each chain execution process
type ChainElementExecution struct {
  ChainConfig        int    `db:"chain_config"`
  ChainID            int    `db:"chain_id"`
  TaskID             int    `db:"task_id"`
  TaskName           string `db:"task_name"`
  Script             string `db:"script"`
  IsSQL              bool   `db:"is_sql"`
  RunUID             string `db:"run_uid"`
  IgnoreError        bool   `db:"ignore_error"`
  DatabaseConnection int    `db:"database_connection"`
  ConnectString      string `db:"connect_string"`
}

// Chain structure used to represent tasks chains
type Chain struct {
  ChainExecutionConfigID   int    `db:"chain_execution_config"`
  ChainID                  int    `db:"chain_id"`
  ChainName                string `db:"chain_name"`
  SelfDestruct             bool   `db:"self_destruct"`
  ExclusiveExecution       bool   `db:"exclusive_execution"`
  ExcludedExecutionConfigs []int  `db:"excluded_execution_configs"`
  MaxInstances             int    `db:"max_instances"`
  TaskType                 string `db:"task_type"`
}

//Run executes jobs
func Run() {
  var doStartUpTasks = true
  var query string

  // create channel for passing chains to workers
  chains := make(chan Chain)
  // create sleeping workers waiting data on channel
  for w := 1; w <= workersNumber; w++ {
    go chainWorker(chains)
  }

  /* cleanup potential database leftovers */
  pgengine.FixSchedulerCrash()
  /* loop forever or until we ask it to stop */
  for {

    if doStartUpTasks {
      /* This is the first task execution after startup, so we will execute one-time tasks... */
      pgengine.LogToDB(0, "log", "checking for startup task chains ...")
      query = " SELECT   chain_execution_config, chain_id, chain_name, " +
        "    self_destruct, exclusive_execution, excluded_execution_configs, " +
        "    COALESCE(max_instances, 999), " +
        "     task_type " +
        " FROM   pg_timetable.chain_execution_config " +
        " WHERE live = 't' " +
        "  AND  task_type = 'S' "
      doStartUpTasks = false
    } else {
      /* ask the database which chains it has to perform */
      pgengine.LogToDB(0, "log", "checking for task chains ...")

      query = " SELECT   chain_execution_config, chain_id, chain_name, " +
        "    self_destruct, exclusive_execution, excluded_execution_configs, " +
        "    COALESCE(max_instances, 999), " +
        "    task_type " +
        " FROM   pg_timetable.chain_execution_config " +
        " WHERE live = 't' AND (task_type <> 'S' OR task_type IS NULL) " +
        "  AND  pg_timetable.check_task(chain_execution_config) = 't' "
    }

    headChains := []Chain{}
    err := pgengine.ConfigDb.Select(&headChains, query)
    if err != nil {
      pgengine.LogToDB(0, "log", "could not query pending tasks:", err)
      return
    }
    pgengine.LogToDB(0, "log", "Number of chain head tuples: ", len(headChains))

    /* now we can loop through so chains */
    for _, headChain := range headChains {
      pgengine.LogToDB(0, "log", fmt.Sprintf("calling process chain for %+v", headChain))
      // put headChain to the channel, then chainWorker will do the magic
      chains <- headChain
    }

    /* wait for the next full minute to show up */
    time.Sleep(60 * time.Second)
  }
}

func chainWorker(chains <-chan Chain) {
  for chain := range chains {
    pgengine.LogToDB(0, "log", fmt.Sprintf("calling process chain for %+v", chain))

    tx := pgengine.ConfigDb.MustBegin()

    query := "SELECT $1 > count(*) " +
      " FROM scheduler.get_running_jobs($2) AS (chain_execution_config int4, start_status int4) " +
      " GROUP BY 1 LIMIT 1"

    for canProceed := false; !canProceed; {
      if err := tx.Get(&canProceed, query, chain.MaxInstances, chain.ChainExecutionConfigID); err != nil {
        pgengine.LogToDB(0, "PANIC", "Application cannot read information concurrent running jobs: ", err)
      }
      time.Sleep(3 * time.Second)
    }

    /* execute a chain */
    executeChain(tx, chain.ChainExecutionConfigID, chain.ChainID)

    /* we can safely check for "self_destruct" here. if we fucked up inside the chain
     * we will never make it to this code here. we would have exited before already.
     * so, if the variable is true, we can start the DB and kill the chain_execution_config. */
    if chain.SelfDestruct {
      tx.MustExec("DELETE FROM timetable.chain_execution_config WHERE chain_execution_config = $1 ",
        chain.ChainExecutionConfigID)
    }

    if err := tx.Commit(); err != nil {
      pgengine.LogToDB(0, "PANIC", "Application cannot commit after job finished: ", err)
    }
  }
}

/* execute a chain of tasks */
func executeChain(tx *sqlx.Tx, chainConfigID int, chainID int) {
  const sqlSelectChains = `WITH RECURSIVE x
  (chain_id, task_id, name, script, is_sql, run_uid, ignore_error, database_connection) AS 
  (
    SELECT tc.chain_id, tc.task_id, bt.name, 
    bt.script, bt.is_sql, 
    tc.run_uid, 
    tc.ignore_error, 
    tc.database_connection 
    FROM scheduler.task_chain tc JOIN 
    scheduler.base_task bt USING (task_id) 
    WHERE tc.parent_id IS NULL AND tc.chain_id = $1 
    UNION ALL 
    SELECT tc.chain_id, tc.task_id, bt.name, 
    bt.script, bt.is_sql, 
    tc.run_uid, 
    tc.ignore_error, 
    tc.database_connection 
    FROM scheduler.task_chain tc JOIN 
    scheduler.base_task bt USING (task_id) JOIN 
    x ON (x.chain_id = tc.parent_id) 
  ) SELECT *, (SELECT connect_string 
      FROM   scheduler.database_connection AS a 
   WHERE a.database_connection = x.database_connection) AS b 
   FROM x`
  const sqlInsertRunStatus = `INSERT INTO scheduler.run_status 
(chain_id, execution_status, started, start_status, chain_execution_config) 
VALUES ($1, 'STARTED', now(), currval('scheduler.run_status_run_status_seq'), $2) 
RETURNING run_status`

  var ChainElements []ChainElementExecution
  var runStatusID int

  pgengine.LogToDB(0, "LOG", "Executing chain: ", chainID)

  /* init the execution run log. we will use to effectively control
   * scheduler concurrency */
  err := tx.Get(&runStatusID, sqlInsertRunStatus, chainID, chainConfigID)
  if err != nil {
    pgengine.LogToDB(0, "ERROR", "Cannot save information about the chain run status: ", err)
  }

  /* execute query */
  err = tx.Select(&ChainElements, sqlSelectChains, chainID)

  if err != nil {
    pgengine.LogToDB(0, "ERROR", "Recursive queries to fetch task chain failed: ", err)
  }

  /* now we can loop through every element of the task chain */
  for _, chainElemExec := range ChainElements {

    retCode := executeСhainElement(chainElemExec)
    pgengine.ConfigDb.MustExec(
      "INSERT INTO scheduler.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
        "is_sql, last_run, finished, returncode, pid) "+
        "VALUES ($1, $2, $3, $4, $5, $6, now(), clock_timestamp(), $7, txid_current())",
      chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
      chainElemExec.Script, chainElemExec.IsSQL, retCode)

    if retCode < 0 {
      tx.MustExec(
        "INSERT INTO scheduler.run_status (chain_id, execution_status, "+
          " current_execution_element, started, last_status_update, start_status, chain_execution_config) "+
          " VALUES ($1, $2, $3, clock_timestamp(), now(), $4, $5)",
        chainElemExec.ChainID, "CHAIN_FAILED", chainElemExec.TaskID, runStatusID, chainConfigID)
      pgengine.LogToDB(0, "ERROR", "Chain execution failed: ", chainElemExec)
    }
  }
}

func executeСhainElement(ChainElemExec ChainElementExecution) int {
  return 0
}

func init() {
  // checkExeExists(walExec, "WAL receiver executable not found!")
  // checkExeExists(baseBackupExec, "Base backup executable not found!")
}
