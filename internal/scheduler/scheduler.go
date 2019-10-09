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
	for !pgengine.TryLockClientName() {
		pgengine.LogToDB("ERROR", "Another client is already connected to server with name: ", pgengine.ClientName)
		time.Sleep(refetchTimeout * time.Second)
	}

	// create channel for passing chains to workers
	chains := make(chan Chain)
	// create sleeping workers waiting data on channel
	for w := 1; w <= workersNumber; w++ {
		go chainWorker(chains)
	}

	/* set maximum connection to workersNumber + 1 for system calls */
	pgengine.ConfigDb.SetMaxOpenConns(workersNumber + 1)

	/* cleanup potential database leftovers */
	pgengine.FixSchedulerCrash()

	/* loop forever or until we ask it to stop */
	for {
		/* ask the database which chains it has to perform */
		pgengine.LogToDB("LOG", "Checking for task chains...")

		query := " SELECT chain_execution_config, chain_id, chain_name, " +
			" self_destruct, exclusive_execution, " +
			" COALESCE(max_instances, 16) as max_instances" +
			" FROM   timetable.chain_execution_config " +
			" WHERE live AND (client_name = $1 or client_name IS NULL) " +
			" AND timetable.check_task(chain_execution_config)"

		headChains := []Chain{}
		err := pgengine.ConfigDb.Select(&headChains, query, pgengine.ClientName)
		if err != nil {
			pgengine.LogToDB("PANIC", "Could not query pending tasks: ", err)
		}
		headChainsCount := len(headChains)
		pgengine.LogToDB("DEBUG", "Number of chain head tuples: ", headChainsCount)

		/* now we can loop through so chains */
		for _, headChain := range headChains {
			if headChainsCount > maxChainsThreshold {
				time.Sleep(time.Duration(refetchTimeout*1000/headChainsCount) * time.Millisecond)
			}
			pgengine.LogToDB("DEBUG", fmt.Sprintf("Putting head chain %s to the execution channel", headChain))
			chains <- headChain
		}

		/* wait for the next full minute to show up */
		time.Sleep(refetchTimeout * time.Second)
	}
}

func chainWorker(chains <-chan Chain) {
	for chain := range chains {
		pgengine.LogToDB("LOG", fmt.Sprintf("Calling process chain for %s", chain))
		for !pgengine.CanProceedChainExecution(chain.ChainExecutionConfigID, chain.MaxInstances) {
			pgengine.LogToDB("DEBUG", fmt.Sprintf("Cannot proceed with chain %s. Sleeping...", chain))
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

	pgengine.LogToDB("LOG", "Executing chain with id: ", chainID)
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
			pgengine.LogToDB("ERROR", fmt.Sprintf("Chain execution failed: %s", chainElemExec))
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
	var execTx *sqlx.Tx
	var remoteDb *sqlx.DB

	pgengine.LogToDB("LOG", fmt.Sprintf("Executing task: %s", chainElemExec))

	if !pgengine.GetChainParamValues(tx, &paramValues, chainElemExec) {
		return -1
	}

	switch chainElemExec.Kind {
	case "SQL":
		execTx = tx
		//Connect to Remote DB
		if chainElemExec.DatabaseConnection.Valid {
			connectionString := pgengine.GetConnectionString(chainElemExec.DatabaseConnection)
			//connection string is empty then don't proceed
			if strings.TrimSpace(connectionString) == "" {
				pgengine.LogToDB("ERROR", fmt.Sprintf("Connection string is blank"))
				return -1
			}
			remoteDb, execTx = pgengine.GetRemoteDBTransaction(connectionString)
			//don't proceed when remote db connection not established
			if execTx == nil {
				pgengine.LogToDB("ERROR", fmt.Sprintf("Couldn't connect to remote database"))
				return -1
			}
			defer pgengine.FinalizeRemoteDBConnection(remoteDb)
		}

		// Set Role
		if chainElemExec.RunUID.Valid {
			pgengine.SetRole(execTx, chainElemExec.RunUID)
		}

		err = pgengine.ExecuteSQLCommand(execTx, chainElemExec.Script, paramValues)

		//Reset The Role
		if chainElemExec.RunUID.Valid {
			pgengine.ResetRole(execTx)
		}

		// Commit changes on remote server
		if chainElemExec.DatabaseConnection.Valid {
			pgengine.MustCommitTransaction(execTx)
		}

	case "SHELL":
		retCode, err = executeShellCommand(chainElemExec.Script, paramValues)
	case "BUILTIN":
		err = tasks.ExecuteTask(chainElemExec.TaskName, paramValues)
	}

	if err != nil {
		pgengine.LogToDB("ERROR", fmt.Sprintf("Task execution failed: %s\n; Error: %s", chainElemExec, err))
		if retCode != 0 {
			return retCode
		}
		return -1
	}

	pgengine.LogToDB("LOG", fmt.Sprintf("Task executed successfully: %s", chainElemExec))

	return 0
}
