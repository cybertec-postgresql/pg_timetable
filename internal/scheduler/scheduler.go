package scheduler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
	"github.com/jmoiron/sqlx"
)

const workersNumber = 16

/* the main loop period. Should be 60 (sec) for release configuration. Set to 10 (sec) for debug purposes */
const refetchTimeout = 60

/* if the number of chains pulled for execution is higher than this value, try to spread execution to avoid spikes */
const maxChainsThreshold = workersNumber * refetchTimeout

//Select live chains with proper client_name value
const sqlSelectLiveChains = `
SELECT
	chain_execution_config, chain_id, chain_name, self_destruct, exclusive_execution, COALESCE(max_instances, 16) as max_instances
FROM 
	timetable.chain_execution_config 
WHERE 
	live AND (client_name = $1 or client_name IS NULL)`

//Select chains to be executed right now()
const sqlSelectChains = sqlSelectLiveChains +
	` AND NOT COALESCE(starts_with(run_at, '@'), FALSE) AND timetable.is_cron_in_time(run_at, now())`

//Select chains to be executed right after reboot
const sqlSelectRebootChains = sqlSelectLiveChains + ` AND run_at = '@reboot'`

// Chain structure used to represent tasks chains
type Chain struct {
	ChainExecutionConfigID int    `db:"chain_execution_config"`
	ChainID                int    `db:"chain_id"`
	ChainName              string `db:"chain_name"`
	SelfDestruct           bool   `db:"self_destruct"`
	ExclusiveExecution     bool   `db:"exclusive_execution"`
	MaxInstances           int    `db:"max_instances"`
}

// create channel for passing chains to workers
var chains chan Chain = make(chan Chain)

func (chain Chain) String() string {
	data, _ := json.Marshal(chain)
	return string(data)
}

//Run executes jobs
func Run() {
	for !pgengine.TryLockClientName() {
		pgengine.LogToDB("ERROR", "Another client is already connected to server with name: ", pgengine.ClientName)
		time.Sleep(refetchTimeout * time.Second)
	}
	// create sleeping workers waiting data on channel
	for w := 1; w <= workersNumber; w++ {
		go chainWorker(chains)
		go intervalChainWorker(intervalChainsChan)
	}
	/* set maximum connection to workersNumber + 1 for system calls */
	pgengine.ConfigDb.SetMaxOpenConns(workersNumber + 1)
	/* cleanup potential database leftovers */
	pgengine.FixSchedulerCrash()
	pgengine.LogToDB("LOG", "Checking for @reboot task chains...")
	retriveChainsAndRun(sqlSelectRebootChains)
	/* loop forever or until we ask it to stop */
	for {
		pgengine.LogToDB("LOG", "Checking for task chains...")
		retriveChainsAndRun(sqlSelectChains)
		pgengine.LogToDB("LOG", "Checking for interval task chains...")
		retriveIntervalChainsAndRun(sqlSelectIntervalChains)
		/* wait for the next full minute to show up */
		time.Sleep(refetchTimeout * time.Second)
	}
}

func retriveChainsAndRun(sql string) {
	headChains := []Chain{}
	err := pgengine.ConfigDb.Select(&headChains, sql, pgengine.ClientName)
	if err != nil {
		pgengine.LogToDB("ERROR", "Could not query pending tasks: ", err)
	} else {
		headChainsCount := len(headChains)
		pgengine.LogToDB("LOG", "Number of chains to be executed: ", headChainsCount)
		/* now we can loop through so chains */
		for _, headChain := range headChains {
			if headChainsCount > maxChainsThreshold {
				time.Sleep(time.Duration(refetchTimeout*1000/headChainsCount) * time.Millisecond)
			}
			pgengine.LogToDB("DEBUG", fmt.Sprintf("Putting head chain %s to the execution channel", headChain))
			chains <- headChain
		}
	}
}

func chainWorker(chains <-chan Chain) {
	for chain := range chains {
		pgengine.LogToDB("DEBUG", fmt.Sprintf("Calling process chain for %s", chain))
		for !pgengine.CanProceedChainExecution(chain.ChainExecutionConfigID, chain.MaxInstances) {
			pgengine.LogToDB("DEBUG", fmt.Sprintf("Cannot proceed with chain %s. Sleeping...", chain))
			time.Sleep(3 * time.Second)
		}

		executeChain(chain.ChainExecutionConfigID, chain.ChainID)
		if chain.SelfDestruct {
			pgengine.DeleteChainConfig(chain.ChainExecutionConfigID)
		}
	}
}

/* execute a chain of tasks */
func executeChain(chainConfigID int, chainID int) {
	var ChainElements []pgengine.ChainElementExecution

	tx := pgengine.StartTransaction()

	pgengine.LogToDB("LOG", fmt.Sprintf("Starting chain ID: %d; configuration ID: %d", chainID, chainConfigID))
	runStatusID := pgengine.InsertChainRunStatus(chainConfigID, chainID)

	if !pgengine.GetChainElements(tx, &ChainElements, chainID) {
		return
	}

	/* now we can loop through every element of the task chain */
	for _, chainElemExec := range ChainElements {
		chainElemExec.ChainConfig = chainConfigID
		pgengine.UpdateChainRunStatus(&chainElemExec, runStatusID, "STARTED")
		retCode := executeСhainElement(tx, &chainElemExec)
		if retCode != 0 && !chainElemExec.IgnoreError {
			pgengine.LogToDB("ERROR", fmt.Sprintf("Chain ID: %d failed", chainID))
			pgengine.UpdateChainRunStatus(&chainElemExec, runStatusID, "CHAIN_FAILED")
			pgengine.MustRollbackTransaction(tx)
			return
		}
		pgengine.UpdateChainRunStatus(&chainElemExec, runStatusID, "CHAIN_DONE")
	}
	pgengine.LogToDB("LOG", fmt.Sprintf("Executed successfully chain ID: %d; configuration ID: %d", chainID, chainConfigID))
	pgengine.UpdateChainRunStatus(
		&pgengine.ChainElementExecution{
			ChainID:     chainID,
			ChainConfig: chainConfigID}, runStatusID, "CHAIN_DONE")
	pgengine.MustCommitTransaction(tx)
}

func executeСhainElement(tx *sqlx.Tx, chainElemExec *pgengine.ChainElementExecution) int {
	var paramValues []string
	var err error
	var out []byte
	var retCode int

	pgengine.LogToDB("DEBUG", fmt.Sprintf("Executing task: %s", chainElemExec))

	if !pgengine.GetChainParamValues(tx, &paramValues, chainElemExec) {
		return -1
	}

	chainElemExec.StartedAt = time.Now()
	switch chainElemExec.Kind {
	case "SQL":
		err = pgengine.ExecuteSQLTask(tx, chainElemExec, paramValues)
	case "SHELL":
		if pgengine.NoShellTasks {
			pgengine.LogToDB("LOG", "Shell task execution skipped: ", chainElemExec)
			return -1
		}
		retCode, out, err = executeShellCommand(chainElemExec.Script, paramValues)
	case "BUILTIN":
		err = tasks.ExecuteTask(chainElemExec.TaskName, paramValues)
	}

	chainElemExec.Duration = time.Since(chainElemExec.StartedAt).Microseconds()
	pgengine.LogChainElementExecution(chainElemExec, retCode, strings.TrimSpace(string(out)))

	if err != nil {
		pgengine.LogToDB("ERROR", fmt.Sprintf("Task execution failed: %s; Error: %s", chainElemExec, err))
		if retCode != 0 {
			return retCode
		}
		return -1
	}

	pgengine.LogToDB("DEBUG", fmt.Sprintf("Task executed successfully: %s", chainElemExec))

	return 0
}
