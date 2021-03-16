package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v4"
)

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

// Lock locks the chain in exclusive or non-exclusive mode
func (sch *Scheduler) Lock(exclusiveExecution bool) {
	if exclusiveExecution {
		sch.exclusiveMutex.Lock()
	} else {
		sch.exclusiveMutex.RLock()
	}
}

// Unlock releases the lock after the chain execution
func (sch *Scheduler) Unlock(exclusiveExecution bool) {
	if exclusiveExecution {
		sch.exclusiveMutex.Unlock()
	} else {
		sch.exclusiveMutex.RUnlock()
	}
}

func (sch *Scheduler) retrieveAsyncChainsAndRun(ctx context.Context) {
	for {
		chainSignal := sch.pgengine.WaitForChainSignal(ctx)
		if chainSignal.ConfigID == 0 {
			return
		}
		switch chainSignal.Command {
		case "START":
			var headChain Chain
			err := sch.pgengine.SelectChain(ctx, &headChain, chainSignal.ConfigID)
			if err != nil {
				sch.pgengine.LogToDB(ctx, "ERROR", "Could not query pending tasks: ", err)
			} else {
				sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Putting head chain %s to the execution channel", headChain))
				sch.chains <- headChain
			}
		case "STOP":
			if cancel, ok := sch.activeChains[chainSignal.ConfigID]; ok {
				cancel()
			}
		}
	}
}

func (sch *Scheduler) retrieveChainsAndRun(ctx context.Context, reboot bool) {
	var err error
	headChains := []Chain{}
	if reboot {
		err = sch.pgengine.SelectRebootChains(ctx, &headChains)
	} else {
		err = sch.pgengine.SelectChains(ctx, &headChains)
	}
	if err != nil {
		sch.pgengine.LogToDB(ctx, "ERROR", "Could not query pending tasks: ", err)
		return
	}
	headChainsCount := len(headChains)
	sch.pgengine.LogToDB(ctx, "LOG", "Number of chains to be executed: ", headChainsCount)
	/* now we can loop through so chains */
	for _, headChain := range headChains {
		if headChainsCount > maxChainsThreshold {
			time.Sleep(time.Duration(refetchTimeout*1000/headChainsCount) * time.Millisecond)
		}
		sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Putting head chain %s to the execution channel", headChain))
		sch.chains <- headChain
	}
}

func (sch *Scheduler) addActiveChain(id int, cancel context.CancelFunc) {
	sch.activeChainMutex.Lock()
	sch.activeChains[id] = cancel
	sch.activeChainMutex.Unlock()
}

func (sch *Scheduler) deleteActiveChain(id int) {
	sch.activeChainMutex.Lock()
	delete(sch.activeChains, id)
	sch.activeChainMutex.Unlock()
}

func (sch *Scheduler) chainWorker(ctx context.Context, chains <-chan Chain) {
	for {
		select {
		case chain := <-chains:
			sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Calling process chain for %s", chain))
			for !sch.pgengine.CanProceedChainExecution(ctx, chain.ChainExecutionConfigID, chain.MaxInstances) {
				sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Cannot proceed with chain %s. Sleeping...", chain))
				select {
				case <-time.After(time.Duration(pgengine.WaitTime) * time.Second):
				case <-ctx.Done():
					return
				}
			}
			sch.Lock(chain.ExclusiveExecution)
			chainContext, cancel := context.WithCancel(ctx)
			sch.addActiveChain(chain.ChainID, cancel)
			sch.executeChain(chainContext, chain.ChainExecutionConfigID, chain.ChainID)
			sch.deleteActiveChain(chain.ChainID)
			cancel()
			sch.Unlock(chain.ExclusiveExecution)
			if chain.SelfDestruct {
				sch.pgengine.DeleteChainConfig(ctx, chain.ChainExecutionConfigID)
			}

		case <-ctx.Done():
			return
		}

	}
}

/* execute a chain of tasks */
func (sch *Scheduler) executeChain(ctx context.Context, chainConfigID int, chainID int) {
	var ChainElements []pgengine.ChainElementExecution

	tx, err := sch.pgengine.StartTransaction(ctx)
	if err != nil {
		sch.pgengine.LogToDB(ctx, "ERROR", fmt.Sprint("Cannot start transaction: ", err))
		return
	}

	sch.pgengine.LogToDB(ctx, "LOG", fmt.Sprintf("Starting chain ID: %d; configuration ID: %d", chainID, chainConfigID))

	if !sch.pgengine.GetChainElements(ctx, tx, &ChainElements, chainID) {
		sch.pgengine.MustRollbackTransaction(ctx, tx)
		return
	}

	runStatusID := sch.pgengine.InsertChainRunStatus(ctx, chainConfigID, chainID)

	/* now we can loop through every element of the task chain */
	for _, chainElemExec := range ChainElements {
		chainElemExec.ChainConfig = chainConfigID
		sch.pgengine.UpdateChainRunStatus(ctx, &chainElemExec, runStatusID, "STARTED")
		retCode := sch.executeСhainElement(ctx, tx, &chainElemExec)
		if retCode != 0 && !chainElemExec.IgnoreError {
			// we use background context here because current one (ctx) might be cancelled
			sch.pgengine.LogToDB(context.Background(), "ERROR", fmt.Sprintf("Chain ID: %d failed", chainID))
			sch.pgengine.UpdateChainRunStatus(context.Background(), &chainElemExec, runStatusID, "CHAIN_FAILED")
			sch.pgengine.MustRollbackTransaction(context.Background(), tx)
			return
		}
		sch.pgengine.UpdateChainRunStatus(context.Background(), &chainElemExec, runStatusID, "CHAIN_DONE")
	}
	sch.pgengine.LogToDB(context.Background(), "LOG", fmt.Sprintf("Executed successfully chain ID: %d; configuration ID: %d", chainID, chainConfigID))
	sch.pgengine.UpdateChainRunStatus(context.Background(),
		&pgengine.ChainElementExecution{
			ChainID:     chainID,
			ChainConfig: chainConfigID}, runStatusID, "CHAIN_DONE")
	sch.pgengine.MustCommitTransaction(ctx, tx)
}

func (sch *Scheduler) executeСhainElement(ctx context.Context, tx pgx.Tx, chainElemExec *pgengine.ChainElementExecution) int {
	var paramValues []string
	var err error
	var out string
	var retCode int

	sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Executing task: %s", chainElemExec))

	if !sch.pgengine.GetChainParamValues(ctx, tx, &paramValues, chainElemExec) {
		return -1
	}

	chainElemExec.StartedAt = time.Now()
	switch chainElemExec.Kind {
	case "SQL":
		err = sch.pgengine.ExecuteSQLTask(ctx, tx, chainElemExec, paramValues)
	case "PROGRAM":
		if sch.pgengine.NoProgramTasks {
			sch.pgengine.LogToDB(ctx, "LOG", "Program task execution skipped: ", chainElemExec)
			return -1
		}
		retCode, out, err = sch.ExecuteProgramCommand(ctx, chainElemExec.Script, paramValues)
	case "BUILTIN":
		err = sch.executeTask(ctx, chainElemExec.TaskName, paramValues)
	}

	chainElemExec.Duration = time.Since(chainElemExec.StartedAt).Microseconds()

	if err != nil {
		if retCode == 0 {
			retCode = -1
		}
		if out == "" {
			out = err.Error()
		}
		sch.pgengine.LogChainElementExecution(context.Background(), chainElemExec, retCode, out)
		sch.pgengine.LogToDB(context.Background(), "ERROR", fmt.Sprintf("Task execution failed: %s; Error: %s", chainElemExec, err))
		return retCode
	}

	sch.pgengine.LogChainElementExecution(ctx, chainElemExec, retCode, out)
	sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Task executed successfully: %s", chainElemExec))
	return 0
}
