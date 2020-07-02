package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/jmoiron/sqlx"
)

const workersNumber = 16

//the main loop period. Should be 60 (sec) for release configuration. Set to 10 (sec) for debug purposes
const refetchTimeout = 60

// if the number of chains pulled for execution is higher than this value, try to spread execution to avoid spikes
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

const sqlSelectSingleChain = sqlSelectLiveChains +
	` AND chain_execution_config = $2`

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
var chains chan Chain = make(chan Chain, workersNumber)

func (chain Chain) String() string {
	data, _ := json.Marshal(chain)
	return string(data)
}

type RunStatus int

const (
	ConnectionDroppped RunStatus = iota
	ContextCancelled
)

//Run executes jobs. Returns Fa
func Run(ctx context.Context, debug bool) RunStatus {
	sysConn, err := stdlib.AcquireConn(pgengine.ConfigDb.DB)
	if err != nil {
		return ConnectionDroppped
	}
	defer func() { _ = stdlib.ReleaseConn(pgengine.ConfigDb.DB, sysConn) }()

	if !pgengine.TryLockClientName(ctx, sysConn) {
		return ContextCancelled
	}

	// create sleeping workers waiting data on channel
	for w := 1; w <= workersNumber; w++ {
		chainCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go chainWorker(chainCtx, chains)
		chainCtx, cancel = context.WithCancel(ctx)
		defer cancel()
		go intervalChainWorker(chainCtx, intervalChainsChan)
	}
	/* set maximum connection to workersNumber + 1 for system calls */
	pgengine.ConfigDb.SetMaxOpenConns(workersNumber)
	/* cleanup potential database leftovers */
	pgengine.FixSchedulerCrash(ctx)

	/*
		Loop forever or until we ask it to stop.
		First loop fetches notifications.
		Main loop works every refetchTimeout seconds and runs chains.
	*/
	pgengine.LogToDB(ctx, "LOG", "Accepting asynchronous chains execution requests...")
	go retrieveAsyncChainsAndRun(ctx)

	if debug { //run blocking notifications receiving
		pgengine.HandleNotifications(ctx)
		return ContextCancelled
	}

	pgengine.LogToDB(ctx, "LOG", "Checking for @reboot task chains...")
	retriveChainsAndRun(ctx, sqlSelectRebootChains, pgengine.ClientName)

	for {
		if !debug {
			pgengine.LogToDB(ctx, "LOG", "Checking for task chains...")
			go retriveChainsAndRun(ctx, sqlSelectChains, pgengine.ClientName)
			pgengine.LogToDB(ctx, "LOG", "Checking for interval task chains...")
			go retriveIntervalChainsAndRun(ctx, sqlSelectIntervalChains)
		}
		select {
		case <-time.After(refetchTimeout * time.Second):
			if sysConn.Ping(ctx) != nil {
				return ConnectionDroppped
			}
		case <-ctx.Done():
			return ContextCancelled
		}
	}
}

func retrieveAsyncChainsAndRun(ctx context.Context) {
	for {
		chainExecutionConfigID := pgengine.WaitForAsyncChain(ctx)
		if chainExecutionConfigID == 0 {
			return
		}
		var headChain Chain
		err := pgengine.ConfigDb.GetContext(ctx, &headChain, sqlSelectSingleChain,
			pgengine.ClientName, chainExecutionConfigID)
		if err != nil {
			pgengine.LogToDB(ctx, "ERROR", "Could not query pending tasks: ", err)
		} else {
			pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Putting head chain %s to the execution channel", headChain))
			chains <- headChain
		}
	}
}

func retriveChainsAndRun(ctx context.Context, sql string, args ...interface{}) {
	headChains := []Chain{}
	err := pgengine.ConfigDb.SelectContext(ctx, &headChains, sql, args...)
	if err != nil {
		pgengine.LogToDB(ctx, "ERROR", "Could not query pending tasks: ", err)
		return
	}
	headChainsCount := len(headChains)
	pgengine.LogToDB(ctx, "LOG", "Number of chains to be executed: ", headChainsCount)
	/* now we can loop through so chains */
	for _, headChain := range headChains {
		if headChainsCount > maxChainsThreshold {
			time.Sleep(time.Duration(refetchTimeout*1000/headChainsCount) * time.Millisecond)
		}
		pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Putting head chain %s to the execution channel", headChain))
		chains <- headChain
	}
}

func chainWorker(ctx context.Context, chains <-chan Chain) {
	for {
		select {
		case chain := <-chains:
			pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Calling process chain for %s", chain))
			for !pgengine.CanProceedChainExecution(ctx, chain.ChainExecutionConfigID, chain.MaxInstances) {
				pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Cannot proceed with chain %s. Sleeping...", chain))
				select {
				case <-time.After(time.Duration(pgengine.WaitTime) * time.Second):
				case <-ctx.Done():
					return
				}
			}
			executeChain(ctx, chain.ChainExecutionConfigID, chain.ChainID)
			if chain.SelfDestruct {
				pgengine.DeleteChainConfig(ctx, chain.ChainExecutionConfigID)
			}
		case <-ctx.Done():
			return
		}

	}
}

/* execute a chain of tasks */
func executeChain(ctx context.Context, chainConfigID int, chainID int) {
	var ChainElements []pgengine.ChainElementExecution

	tx, err := pgengine.StartTransaction(ctx)
	if err != nil {
		pgengine.LogToDB(ctx, "ERROR", fmt.Sprint("Cannot start transaction: ", err))
		return
	}

	pgengine.LogToDB(ctx, "LOG", fmt.Sprintf("Starting chain ID: %d; configuration ID: %d", chainID, chainConfigID))

	if !pgengine.GetChainElements(ctx, tx, &ChainElements, chainID) {
		pgengine.MustRollbackTransaction(ctx, tx)
		return
	}

	runStatusID := pgengine.InsertChainRunStatus(ctx, chainConfigID, chainID)

	/* now we can loop through every element of the task chain */
	for _, chainElemExec := range ChainElements {
		chainElemExec.ChainConfig = chainConfigID
		pgengine.UpdateChainRunStatus(ctx, &chainElemExec, runStatusID, "STARTED")
		retCode := executeСhainElement(ctx, tx, &chainElemExec)
		if retCode != 0 && !chainElemExec.IgnoreError {
			pgengine.LogToDB(ctx, "ERROR", fmt.Sprintf("Chain ID: %d failed", chainID))
			pgengine.UpdateChainRunStatus(ctx, &chainElemExec, runStatusID, "CHAIN_FAILED")
			pgengine.MustRollbackTransaction(ctx, tx)
			return
		}
		pgengine.UpdateChainRunStatus(ctx, &chainElemExec, runStatusID, "CHAIN_DONE")
	}
	pgengine.LogToDB(ctx, "LOG", fmt.Sprintf("Executed successfully chain ID: %d; configuration ID: %d", chainID, chainConfigID))
	pgengine.UpdateChainRunStatus(ctx,
		&pgengine.ChainElementExecution{
			ChainID:     chainID,
			ChainConfig: chainConfigID}, runStatusID, "CHAIN_DONE")
	pgengine.MustCommitTransaction(ctx, tx)
}

func executeСhainElement(ctx context.Context, tx *sqlx.Tx, chainElemExec *pgengine.ChainElementExecution) int {
	var paramValues []string
	var err error
	var out string
	var retCode int

	pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Executing task: %s", chainElemExec))

	if !pgengine.GetChainParamValues(ctx, tx, &paramValues, chainElemExec) {
		return -1
	}

	chainElemExec.StartedAt = time.Now()
	switch chainElemExec.Kind {
	case "SQL":
		err = pgengine.ExecuteSQLTask(ctx, tx, chainElemExec, paramValues)
	case "SHELL":
		if pgengine.NoShellTasks {
			pgengine.LogToDB(ctx, "LOG", "Shell task execution skipped: ", chainElemExec)
			return -1
		}
		retCode, out, err = ExecuteShellCommand(ctx, chainElemExec.Script, paramValues)
	case "BUILTIN":
		err = tasks.ExecuteTask(ctx, chainElemExec.TaskName, paramValues)
	}

	chainElemExec.Duration = time.Since(chainElemExec.StartedAt).Microseconds()

	if err != nil {
		if retCode == 0 {
			retCode = -1
		}
		if out == "" {
			out = err.Error()
		}
		pgengine.LogChainElementExecution(ctx, chainElemExec, retCode, out)
		pgengine.LogToDB(ctx, "ERROR", fmt.Sprintf("Task execution failed: %s; Error: %s", chainElemExec, err))
		return retCode
	}

	pgengine.LogChainElementExecution(ctx, chainElemExec, retCode, out)
	pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Task executed successfully: %s", chainElemExec))
	return 0
}
