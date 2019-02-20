package scheduler

import (
  "fmt"
  "os/exec"
  "time"

  "github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
  "github.com/jmoiron/sqlx"
)

const workersNumber = 16

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
      pgengine.LogToDB("log", "checking for startup task chains ...")
      query = " SELECT   chain_execution_config, chain_id, chain_name, " +
        "    self_destruct, exclusive_execution, excluded_execution_configs, " +
        "    COALESCE(max_instances, 999), " +
        "     task_type " +
        " FROM   timetable.chain_execution_config " +
        " WHERE live = 't' " +
        "  AND  task_type = 'S' "
      doStartUpTasks = false
    } else {
      /* ask the database which chains it has to perform */
      pgengine.LogToDB("log", "checking for task chains ...")

      query = " SELECT   chain_execution_config, chain_id, chain_name, " +
        "    self_destruct, exclusive_execution, excluded_execution_configs, " +
        "    COALESCE(max_instances, 999), " +
        "    task_type " +
        " FROM   timetable.chain_execution_config " +
        " WHERE live = 't' AND (task_type <> 'S' OR task_type IS NULL) " +
        "  AND  timetable.check_task(chain_execution_config) = 't' "
    }

    headChains := []Chain{}
    err := pgengine.ConfigDb.Select(&headChains, query)
    if err != nil {
      pgengine.LogToDB("LOG", "could not query pending tasks:", err)
      return
    }
    pgengine.LogToDB("LOG", "Number of chain head tuples: ", len(headChains))

    /* now we can loop through so chains */
    for _, headChain := range headChains {
      pgengine.LogToDB("LOG", fmt.Sprintf("Сalling process chain for %+v", headChain))
      // put headChain to the channel, then chainWorker will do the magic
      chains <- headChain
    }

    /* wait for the next full minute to show up */
    time.Sleep(60 * time.Second)
  }
}

func chainWorker(chains <-chan Chain) {
  for chain := range chains {
    pgengine.LogToDB("log", fmt.Sprintf("calling process chain for %+v", chain))
    for !pgengine.CanProceedChainExecution(chain.ChainExecutionConfigID, chain.MaxInstances) {
      time.Sleep(3 * time.Second)
    }
    tx := pgengine.StartTransaction()
    executeChain(tx, chain.ChainExecutionConfigID, chain.ChainID)
    if chain.SelfDestruct {
      pgengine.DeleteChainConfig(tx, chain.ChainExecutionConfigID)
    }
    pgengine.MustCommitTransaction(tx)
  }
}

/* execute a chain of tasks */
func executeChain(tx *sqlx.Tx, chainConfigID int, chainID int) {

  const sqlInsertRunStatus = `INSERT INTO timetable.run_status 
(chain_id, execution_status, started, start_status, chain_execution_config) 
VALUES ($1, 'STARTED', now(), currval('timetable.run_status_run_status_seq'), $2) 
RETURNING run_status`

  const sqlInsertFinishStatus = `INSERT INTO timetable.run_status 
(chain_id, execution_status, current_execution_element, started, last_status_update, start_status, chain_execution_config)
VALUES ($1, $2, $3, clock_timestamp(), now(), $4, $5)`

  var ChainElements []pgengine.ChainElementExecution
  var runStatusID int

  pgengine.LogToDB("LOG", "Executing chain: ", chainID)

  /* init the execution run log. we will use to effectively control
   * scheduler concurrency */
  err := tx.Get(&runStatusID, sqlInsertRunStatus, chainID, chainConfigID)
  if err != nil {
    pgengine.LogToDB("ERROR", "Cannot save information about the chain run status: ", err)
  }

  if err := pgengine.GetChainElements(tx, &ChainElements, chainID); err != nil {
    return
  }

  /* now we can loop through every element of the task chain */
  for _, chainElemExec := range ChainElements {
    chainElemExec.ChainID = chainID
    tx.MustExec(sqlInsertFinishStatus, chainID, "RUNNING", chainElemExec.TaskID, runStatusID, chainConfigID)
    retCode := executeСhainElement(tx, chainElemExec)
    pgengine.ConfigDb.MustExec(
      "INSERT INTO timetable.execution_log (chain_execution_config, chain_id, task_id, name, script, "+
        "is_sql, last_run, finished, returncode, pid) "+
        "VALUES ($1, $2, $3, $4, $5, $6, now(), clock_timestamp(), $7, txid_current())",
      chainElemExec.ChainConfig, chainElemExec.ChainID, chainElemExec.TaskID, chainElemExec.TaskName,
      chainElemExec.Script, chainElemExec.IsSQL, retCode)

    if retCode < 0 {
      tx.MustExec(sqlInsertFinishStatus, chainElemExec.ChainID, "FAILED",
        chainElemExec.TaskID, runStatusID, chainConfigID)
      pgengine.LogToDB("ERROR", "Chain execution failed: ", chainElemExec)
      return
    }

    tx.MustExec(sqlInsertFinishStatus, chainElemExec.ChainID, "SUCCESS",
      chainElemExec.TaskID, runStatusID, chainConfigID)
  }
  tx.MustExec(sqlInsertFinishStatus, chainID, "CHAIN_DONE", nil, runStatusID, chainConfigID)
}

func executeСhainElement(tx *sqlx.Tx, ChainElemExec pgengine.ChainElementExecution) int {
  const sqlGetParamValues = `SELECT value
FROM  timetable.chain_execution_parameters
WHERE chain_execution_config = $1
  AND chain_id = $2
ORDER BY order_id ASC`
  var err error

  pgengine.LogToDB("LOG", fmt.Sprintf(
    "Executing task id: %d, chain_id: %d: task_name: %s, is_sql: %t",
    ChainElemExec.TaskID, ChainElemExec.ChainID, ChainElemExec.TaskName, ChainElemExec.IsSQL))

  var paramValues []string
  err = tx.Select(&paramValues, sqlGetParamValues, ChainElemExec.ChainConfig, ChainElemExec.ChainID)
  if err != nil {
    pgengine.LogToDB("ERROR", "Cannot fetch parameters values for chain: ", err)
    return -1
  }

  pgengine.LogToDB("LOG", fmt.Sprintf(
    "Parameters found for task id: %d, chain_id: %d: task_name: %s, is_sql: %t",
    ChainElemExec.TaskID, ChainElemExec.ChainID, ChainElemExec.TaskName, ChainElemExec.IsSQL))

  if ChainElemExec.IsSQL {
    _, err = tx.Exec(ChainElemExec.Script, paramValues)
  } else {
    command := exec.Command(ChainElemExec.Script, paramValues...) // #nosec
    err = command.Run()
  }
  if err != nil {
    pgengine.LogToDB("ERROR", fmt.Sprintf(
      "Chain execution failed for task id: %d, chain_id: %d: task_name: %s, is_sql: %t",
      ChainElemExec.TaskID, ChainElemExec.ChainID, ChainElemExec.TaskName, ChainElemExec.IsSQL))
    return -1
  }

  pgengine.LogToDB("LOG", fmt.Sprintf(
    "Chain executed successfully for task id: %d, chain_id: %d: task_name: %s, is_sql: %t",
    ChainElemExec.TaskID, ChainElemExec.ChainID, ChainElemExec.TaskName, ChainElemExec.IsSQL))
  return 0
}

func init() {
  // checkExeExists(walExec, "WAL receiver executable not found!")
  // checkExeExists(baseBackupExec, "Base backup executable not found!")
}
