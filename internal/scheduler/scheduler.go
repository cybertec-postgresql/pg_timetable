package scheduler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
	"github.com/jmoiron/sqlx"
)

const workersNumber = 16
/* the main loop period. Should be 60 (sec) for release configuration. Now is 10 (sec) for debug purposes */
const refetchTimeout = 10

// Chain structure used to represent tasks chains
type Chain struct {
	ChainExecutionConfigID int    `db:"chain_execution_config"`
	ChainID                int    `db:"chain_id"`
	ChainName              string `db:"chain_name"`
	SelfDestruct           bool   `db:"self_destruct"`
	ExclusiveExecution     bool   `db:"exclusive_execution"`
	MaxInstances           int    `db:"max_instances"`
}

func (chain Chain) String() string {
	data, _ := json.Marshal(chain)
	return string(data)
}

//Run executes jobs
func Run() {
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

		/* ask the database which chains it has to perform */
		pgengine.LogToDB("LOG", "checking for task chains ...")

		query = " SELECT chain_execution_config, chain_id, chain_name, " +
			" self_destruct, exclusive_execution, " +
			" COALESCE(max_instances, 16) as max_instances" +
			" FROM   timetable.chain_execution_config " +
			" WHERE live AND timetable.check_task(chain_execution_config)"

		headChains := []Chain{}
		err := pgengine.ConfigDb.Select(&headChains, query)
		if err != nil {
			pgengine.LogToDB("PANIC", "could not query pending tasks:", err)
			return
		}
		pgengine.LogToDB("DEBUG", "number of chain head tuples: ", len(headChains))

		/* now we can loop through so chains */
		for _, headChain := range headChains {
			pgengine.LogToDB("DEBUG", fmt.Sprintf("putting head chain %s to the execution channel", headChain))
			chains <- headChain
		}

		/* wait for the next full minute to show up */
		time.Sleep(refetchTimeout * time.Second)
	}
}

func chainWorker(chains <-chan Chain) {
	for chain := range chains {
		pgengine.LogToDB("LOG", fmt.Sprintf("calling process chain for %s", chain))
		for !pgengine.CanProceedChainExecution(chain.ChainExecutionConfigID, chain.MaxInstances) {
			pgengine.LogToDB("DEBUG", fmt.Sprintf("cannot proceed with chain %s. Sleeping...", chain))
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

	pgengine.LogToDB("LOG", "executing chain with id: ", chainID)
	runStatusID := pgengine.InsertChainRunStatus(tx, chainConfigID, chainID)

	if !pgengine.GetChainElements(tx, &ChainElements, chainID) {
		return
	}

	/* now we can loop through every element of the task chain */
	for _, chainElemExec := range ChainElements {
		chainElemExec.ChainConfig = chainConfigID
		pgengine.UpdateChainRunStatus(tx, &chainElemExec, runStatusID, "STARTED")
		retCode := executeСhainElement(tx, &chainElemExec)
		pgengine.LogChainElementExecution(&chainElemExec, retCode)
		if retCode < 0 {
			pgengine.UpdateChainRunStatus(tx, &chainElemExec, runStatusID, "CHAIN_FAILED")
			pgengine.LogToDB("ERROR", fmt.Sprintf("chain execution failed: %s", chainElemExec))
			return
		}
		pgengine.UpdateChainRunStatus(tx, &chainElemExec, runStatusID, "CHAIN_DONE")
	}
	pgengine.UpdateChainRunStatus(tx,
		&pgengine.ChainElementExecution{
			ChainID:     chainID,
			ChainConfig: chainConfigID}, runStatusID, "CHAIN_DONE")
}

func executeСhainElement(tx *sqlx.Tx, chainElemExec *pgengine.ChainElementExecution) int {
	var paramValues []string
	var err error
	var retCode int

	pgengine.LogToDB("LOG", fmt.Sprintf("executing task: %s", chainElemExec))

	if !pgengine.GetChainParamValues(tx, &paramValues, chainElemExec) {
		return -1
	}

	switch chainElemExec.Kind {
	case "SQL":
		err = pgengine.ExecuteSQLCommand(tx, chainElemExec.Script, paramValues)
	case "SHELL":
		err, retCode = executeShellCommand(chainElemExec.Script, paramValues)
	case "BUILTIN":
		err = tasks.ExecuteTask(chainElemExec.TaskName, paramValues)
	}

	if err != nil {
		pgengine.LogToDB("ERROR", fmt.Sprintf("task execution failed: %s\n; Error: %s", chainElemExec, err))
		if retCode != 0 {
			return retCode
		}
		return -1
	}

	pgengine.LogToDB("LOG", fmt.Sprintf("task executed successfully: %s", chainElemExec))
	return 0
}
