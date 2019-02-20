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
  var ChainElements []pgengine.ChainElementExecution

  pgengine.LogToDB("LOG", "Executing chain: ", chainID)
  runStatusID := pgengine.InsertChainRunStatus(tx, chainConfigID, chainID)

  if !pgengine.GetChainElements(tx, &ChainElements, chainID) {
    return
  }

  /* now we can loop through every element of the task chain */
  for _, chainElemExec := range ChainElements {
    chainElemExec.ChainConfig = chainConfigID
    pgengine.UpdateChainRunStatus(tx, &chainElemExec, runStatusID, "RUNNING")
    retCode := executeСhainElement(tx, &chainElemExec)
    pgengine.LogChainElementExecution(&chainElemExec, retCode)
    if retCode < 0 {
      pgengine.UpdateChainRunStatus(tx, &chainElemExec, runStatusID, "FAILED")
      pgengine.LogToDB("ERROR", "Chain execution failed: ", chainElemExec)
      return
    }
    pgengine.UpdateChainRunStatus(tx, &chainElemExec, runStatusID, "SUCCESS")
  }
  pgengine.UpdateChainRunStatus(tx,
    &pgengine.ChainElementExecution{
      ChainID:     chainID,
      ChainConfig: chainConfigID}, runStatusID, "CHAIN_DONE")
}

func executeСhainElement(tx *sqlx.Tx, chainElemExec *pgengine.ChainElementExecution) int {
  const sqlGetParamValues = `SELECT value
FROM  timetable.chain_execution_parameters
WHERE chain_execution_config = $1
  AND chain_id = $2
ORDER BY order_id ASC`
  var paramValues []string
  var err error

  pgengine.LogToDB("LOG", fmt.Sprintf("Executing task: %v", chainElemExec))

  if !pgengine.GetChainParamValues(tx, &paramValues, chainElemExec) {
    return -1
  }

  pgengine.LogToDB("LOG", fmt.Sprintf("Parameters found for task id: %v", chainElemExec))

  if chainElemExec.IsSQL {
    _, err = tx.Exec(chainElemExec.Script, paramValues)
  } else {
    command := exec.Command(chainElemExec.Script, paramValues...) // #nosec
    err = command.Run()
  }
  if err != nil {
    pgengine.LogToDB("ERROR", fmt.Sprintf(
      "Chain execution failed for task id: %d, chain_id: %d: task_name: %s, is_sql: %t",
      chainElemExec.TaskID, chainElemExec.ChainID, chainElemExec.TaskName, chainElemExec.IsSQL))
    return -1
  }

  pgengine.LogToDB("LOG", fmt.Sprintf(
    "Chain executed successfully for task id: %d, chain_id: %d: task_name: %s, is_sql: %t",
    chainElemExec.TaskID, chainElemExec.ChainID, chainElemExec.TaskName, chainElemExec.IsSQL))
  return 0
}

func init() {
  // checkExeExists(walExec, "WAL receiver executable not found!")
  // checkExeExists(baseBackupExec, "Base backup executable not found!")
}
