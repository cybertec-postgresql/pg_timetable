package scheduler

import (
	"context"
	"strings"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v4"
)

// Chain structure used to represent tasks chains
type Chain struct {
	ChainID            int    `db:"chain_id"`
	ChainName          string `db:"chain_name"`
	SelfDestruct       bool   `db:"self_destruct"`
	ExclusiveExecution bool   `db:"exclusive_execution"`
	MaxInstances       int    `db:"max_instances"`
	Timeout            int    `db:"timeout"`
	RunStatusID        int    `db:"run_status_id"`
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
				sch.l.WithError(err).Error("Could not query pending tasks")
			} else {
				select {
				case sch.chainsChan <- headChain:
					sch.l.WithField("chain", headChain.ChainID).Debug("Sent head chain to the execution channel")
				default:
					sch.l.WithField("chain", headChain.ChainID).Error("Failed to send head chain to the execution channel")
				}
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
	msg := "Retrieve scheduled chains to run"
	if reboot {
		msg = msg + " @reboot"
	}
	headChains := []Chain{}
	if reboot {
		err = sch.pgengine.SelectRebootChains(ctx, &headChains)
	} else {
		err = sch.pgengine.SelectChains(ctx, &headChains)
	}
	if err != nil {
		sch.l.WithError(err).Error("Could not query pending tasks")
		return
	}
	headChainsCount := len(headChains)
	sch.l.WithField("count", headChainsCount).Info(msg)
	// now we can loop through so chains
	for _, headChain := range headChains {
		// if the number of chains pulled for execution is high, try to spread execution to avoid spikes
		if headChainsCount > sch.Config().Resource.CronWorkers*refetchTimeout {
			time.Sleep(time.Duration(refetchTimeout*1000/headChainsCount) * time.Millisecond)
		}
		select {
		case sch.chainsChan <- headChain:
			sch.l.WithField("chain", headChain.ChainID).Debug("Sent head chain to the execution channel")
		default:
			sch.l.WithField("chain", headChain.ChainID).Error("Failed to send head chain to the execution channel")
		}
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

func (sch *Scheduler) terminateChains() {
	for id, cancel := range sch.activeChains {
		sch.l.WithField("chain", id).Debug("Terminating chain...")
		cancel()
	}
	for {
		time.Sleep(1 * time.Second) // give some time to terminate chains gracefully
		if len(sch.activeChains) == 0 {
			return
		}
		sch.l.Debugf("Still active chains running: %d", len(sch.activeChains))
	}
}

func (sch *Scheduler) chainWorker(ctx context.Context, chains <-chan Chain) {
	for {
		select {
		case <-ctx.Done(): //check context with high priority
			return
		default:
			select {
			case chain := <-chains:
				chainL := sch.l.WithField("chain", chain.ChainID)
				chainContext := log.WithLogger(ctx, chainL)
				chain.RunStatusID = sch.pgengine.InsertChainRunStatus(ctx, chain.ChainID)
				if chain.RunStatusID == -1 {
					chainL.Info("Cannot proceed. Sleeping")
					continue
				}
				chainL.Info("Starting chain")
				sch.Lock(chain.ExclusiveExecution)
				chainContext, cancel := context.WithCancel(chainContext)
				sch.addActiveChain(chain.ChainID, cancel)
				sch.executeChain(chainContext, chain)
				sch.deleteActiveChain(chain.ChainID)
				cancel()
				sch.Unlock(chain.ExclusiveExecution)
			case <-ctx.Done():
				return
			}

		}
	}
}

func getTimeoutContext(ctx context.Context, t1 int, t2 int) (context.Context, context.CancelFunc) {
	var timeout int
	if t1 > t2 {
		timeout = t1
	} else {
		timeout = t2
	}
	if timeout > 0 {
		return context.WithTimeout(ctx, time.Millisecond*time.Duration(timeout))
	}
	return ctx, nil
}

/* execute a chain of tasks */
func (sch *Scheduler) executeChain(ctx context.Context, chain Chain) {
	var ChainTasks []pgengine.ChainTask
	var bctx context.Context
	var cancel context.CancelFunc
	var status string

	ctx, cancel = getTimeoutContext(ctx, sch.Config().Resource.ChainTimeout, chain.Timeout)
	if cancel != nil {
		defer cancel()
	}

	chainL := sch.l.WithField("chain", chain.ChainID)

	tx, err := sch.pgengine.StartTransaction(ctx)
	if err != nil {
		chainL.WithError(err).Error("Cannot start transaction")
		return
	}

	if !sch.pgengine.GetChainElements(ctx, tx, &ChainTasks, chain.ChainID) {
		sch.pgengine.RollbackTransaction(ctx, tx)
		return
	}

	/* now we can loop through every element of the task chain */
	for i, task := range ChainTasks {
		task.ChainID = chain.ChainID
		l := chainL.WithField("task", task.TaskID)
		l.Info("Starting task")
		ctx = log.WithLogger(ctx, l)
		sch.pgengine.AddChainRunStatus(ctx, &task, chain.RunStatusID, "TASK_STARTED")
		retCode := sch.executeСhainElement(ctx, tx, &task)

		// we use background context here because current one (ctx) might be cancelled
		bctx = log.WithLogger(context.Background(), l)
		if retCode != 0 {
			if !task.IgnoreError {
				chainL.Error("Chain failed")
				sch.pgengine.AddChainRunStatus(bctx, &task, chain.RunStatusID, "CHAIN_FAILED")
				sch.pgengine.RollbackTransaction(bctx, tx)
				return
			}
			l.Info("Ignoring task failure")
		}
		if i == len(ChainTasks)-1 {
			status = "CHAIN_DONE"
		} else {
			status = "TASK_DONE"
		}
		sch.pgengine.AddChainRunStatus(bctx, &task, chain.RunStatusID, status)
	}
	chainL.Info("Chain executed successfully")
	bctx = log.WithLogger(context.Background(), chainL)
	sch.pgengine.CommitTransaction(bctx, tx)
	if chain.SelfDestruct {
		sch.pgengine.DeleteChainConfig(bctx, chain.ChainID)
	}
}

func (sch *Scheduler) executeСhainElement(ctx context.Context, tx pgx.Tx, task *pgengine.ChainTask) int {
	var (
		paramValues []string
		err         error
		out         string
		retCode     int
		cancel      context.CancelFunc
	)

	l := log.GetLogger(ctx)
	if !sch.pgengine.GetChainParamValues(ctx, tx, &paramValues, task) {
		return -1
	}

	ctx, cancel = getTimeoutContext(ctx, sch.Config().Resource.TaskTimeout, task.Timeout)
	if cancel != nil {
		defer cancel()
	}

	task.StartedAt = time.Now()
	switch task.Kind {
	case "SQL":
		out, err = sch.pgengine.ExecuteSQLTask(ctx, tx, task, paramValues)
	case "PROGRAM":
		if sch.pgengine.NoProgramTasks {
			l.Info("Program task execution skipped")
			return -2
		}
		retCode, out, err = sch.ExecuteProgramCommand(ctx, task.Script, paramValues)
	case "BUILTIN":
		out, err = sch.executeTask(ctx, task.Script, paramValues)
	}
	task.Duration = time.Since(task.StartedAt).Microseconds()

	if err != nil {
		if retCode == 0 {
			retCode = -1
		}
		out = strings.Join([]string{out, err.Error()}, "\n")
		l.WithError(err).Error("Task execution failed")
	} else {
		l.Info("Task executed successfully")
	}
	sch.pgengine.LogChainElementExecution(context.Background(), task, retCode, out)
	return retCode
}
