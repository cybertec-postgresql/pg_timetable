package scheduler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	pgx "github.com/jackc/pgx/v5"
)

type (
	Chain       = pgengine.Chain
	ChainSignal = pgengine.ChainSignal
)

// SendChain sends chain to the channel for workers
func (sch *Scheduler) SendChain(c Chain) {
	select {
	case sch.chainsChan <- c:
		sch.l.WithField("chain", c.ChainID).Debug("Sent chain to the execution channel")
	default:
		sch.l.WithField("chain", c.ChainID).Error("Failed to send chain to the execution channel")
	}
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
		err := sch.processAsyncChain(ctx, chainSignal)
		if err != nil {
			sch.l.WithError(err).Error("Could not process async chain command")
		}
	}
}

func (sch *Scheduler) processAsyncChain(ctx context.Context, chainSignal ChainSignal) error {
	switch chainSignal.Command {
	case "START":
		var c Chain
		if err := sch.pgengine.SelectChain(ctx, &c, chainSignal.ConfigID); err != nil {
			return fmt.Errorf("cannot start chain with ID: %d; %w", chainSignal.ConfigID, err)
		}
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(chainSignal.Delay) * time.Second):
				sch.SendChain(c)
			}
		}()
	case "STOP":
		if cancel, ok := sch.activeChains[chainSignal.ConfigID]; ok {
			cancel()
			return nil
		}
		return fmt.Errorf("cannot stop chain with ID: %d. No running chain found", chainSignal.ConfigID)
	}
	return nil
}

func (sch *Scheduler) retrieveChainsAndRun(ctx context.Context, reboot bool) {
	var err error
	var headChains []Chain
	msg := "Retrieve scheduled chains to run"
	if reboot {
		msg += " @reboot"
	}
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
	// now we can loop through the chains
	for _, c := range headChains {
		// if the number of chains pulled for execution is high, try to spread execution to avoid spikes
		if headChainsCount > sch.Config().Resource.CronWorkers*refetchTimeout {
			time.Sleep(time.Duration(refetchTimeout*1000/headChainsCount) * time.Millisecond)
		}
		sch.SendChain(c)
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
				if !sch.pgengine.InsertChainRunStatus(ctx, chain.ChainID, chain.MaxInstances) {
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

func getTimeoutContext(ctx context.Context, globalTimeout int, customTimeout int) (context.Context, context.CancelFunc) {
	timeout := cmp.Or(customTimeout, globalTimeout)
	if timeout > 0 {
		return context.WithTimeout(ctx, time.Millisecond*time.Duration(timeout))
	}
	return ctx, nil
}

func (sch *Scheduler) executeOnErrorHandler(ctx context.Context, chain Chain) {
	if ctx.Err() != nil || chain.OnError == "" {
		return
	}
	l := sch.l.WithField("chain", chain.ChainID)
	l.Info("Starting error handling")
	if _, err := sch.pgengine.ConfigDb.Exec(ctx, chain.OnError); err != nil {
		l.Info("Error handler failed")
		return
	}
	l.Info("Error handler executed successfully")
}

/* execute a chain of tasks */
func (sch *Scheduler) executeChain(ctx context.Context, chain Chain) {
	var ChainTasks []pgengine.ChainTask
	var bctx context.Context
	var cancel context.CancelFunc
	var vxid int64

	chainCtx, cancel := getTimeoutContext(ctx, sch.Config().Resource.ChainTimeout, chain.Timeout)
	if cancel != nil {
		defer cancel()
	}

	chainL := sch.l.WithField("chain", chain.ChainID)
	tx, vxid, err := sch.pgengine.StartTransaction(chainCtx)
	if err != nil {
		chainL.WithError(err).Error("Cannot start transaction")
		return
	}
	chainL = chainL.WithField("vxid", vxid)

	err = sch.pgengine.GetChainElements(chainCtx, &ChainTasks, chain.ChainID)
	if err != nil {
		chainL.WithError(err).Error("Failed to retrieve chain elements")
		sch.pgengine.RollbackTransaction(chainCtx, tx)
		return
	}

	/* now we can loop through every element of the task chain */
	for _, task := range ChainTasks {
		task.ChainID = chain.ChainID
		task.Vxid = vxid
		l := chainL.WithField("task", task.TaskID)
		l.Info("Starting task")
		taskCtx := log.WithLogger(chainCtx, l)
		err = sch.executeTask(taskCtx, tx, &task)
		if err != nil {
			l.WithError(err).Error("Task execution failed")
		} else {
			l.Info("Task executed successfully")
		}

		// we use background context here because current one (chainCtx) might be cancelled
		bctx = log.WithLogger(ctx, l)
		if err != nil {
			if !task.IgnoreError {
				chainL.Error("Chain failed")
				sch.pgengine.RemoveChainRunStatus(bctx, chain.ChainID)
				sch.pgengine.RollbackTransaction(bctx, tx)
				sch.executeOnErrorHandler(bctx, chain)
				return
			}
			l.Info("Ignoring task failure")
		}
	}
	bctx = log.WithLogger(chainCtx, chainL)
	sch.pgengine.CommitTransaction(bctx, tx)
	chainL.Info("Chain executed successfully")
	sch.pgengine.RemoveChainRunStatus(bctx, chain.ChainID)
	if chain.SelfDestruct {
		sch.pgengine.DeleteChain(bctx, chain.ChainID)
	}
}

/* execute a task */
func (sch *Scheduler) executeTask(ctx context.Context, tx pgx.Tx, task *pgengine.ChainTask) error {
	var (
		paramValues []string
		err         error
		cancel      context.CancelFunc
	)

	l := log.GetLogger(ctx)
	err = sch.pgengine.GetChainParamValues(ctx, &paramValues, task)
	if err != nil {
		l.WithError(err).Error("cannot fetch parameters values for chain: ", err)
		return err
	}

	ctx, cancel = getTimeoutContext(ctx, sch.Config().Resource.TaskTimeout, task.Timeout)
	if cancel != nil {
		defer cancel()
	}

	task.StartedAt = time.Now()
	switch task.Kind {
	case "SQL":
		err = sch.pgengine.ExecuteSQLTask(ctx, tx, task, paramValues)
	case "PROGRAM":
		if sch.pgengine.NoProgramTasks {
			l.Info("Program task execution skipped")
			return errors.New("program tasks execution is disabled")
		}
		err = sch.ExecuteProgramCommand(ctx, task, paramValues)
	case "BUILTIN":
		err = sch.executeBuiltinTask(ctx, task, paramValues)
	}
	task.Duration = time.Since(task.StartedAt).Microseconds()
	return err
}
