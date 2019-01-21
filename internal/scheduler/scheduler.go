package scheduler

import (
  "fmt"
  "time"

  "github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

const workersNumber = 16

// type ChainElementExecution struct {
//   chain_config        int
//   chain_id            int
//   task_id             int
//   task_name           string
//   script              string
//   is_sql              string
//   run_uid             string
//   ignore_error        string
//   database_connection int
//   connect_string      string
// }

// Chain structure used to represent tasks chains
type Chain struct {
  ChainExecutionConfig     string `db:"chain_execution_config"`
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
      if err := tx.Get(&canProceed, query, chain.MaxInstances, chain.ChainExecutionConfig); err != nil {
        pgengine.LogToDB(0, "PANIC", "Application cannot read information concurrent running jobs: ", err)
      }

      time.Sleep(3 * time.Second)
    }

    /* execute a chain */
    //execute_chain(chain_config, start_chain_id)

    /* we can safely check for "self_destruct" here. if we fucked up inside the chain
     * we will never make it to this code here. we would have exited before already.
     * so, if the variable is true, we can start the DB and kill the chain_execution_config. */
    if chain.SelfDestruct {
      tx.MustExec("DELETE FROM timetable.chain_execution_config WHERE chain_execution_config = $1 ",
        chain.ChainExecutionConfig)
    }

    if err := tx.Commit(); err != nil {
      pgengine.LogToDB(0, "PANIC", "Application cannot commit after job finished: ", err)
    }
  }
}

func init() {
  // checkExeExists(walExec, "WAL receiver executable not found!")
  // checkExeExists(baseBackupExec, "Base backup executable not found!")
}
