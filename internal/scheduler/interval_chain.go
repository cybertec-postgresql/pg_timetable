package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

// IntervalChain structure used to represent repeated chains.
type IntervalChain struct {
	Chain
	Interval    int  `db:"interval_seconds"`
	RepeatAfter bool `db:"repeat_after"`
}

func (ichain IntervalChain) isListed(ichains []IntervalChain) bool {
	for _, ic := range ichains {
		if ichain.ChainExecutionConfigID == ic.ChainExecutionConfigID {
			return true
		}
	}
	return false
}

func (sch *Scheduler) isValid(ichain IntervalChain) bool {
	return (IntervalChain{}) != sch.intervalChains[ichain.ChainExecutionConfigID]
}

func (sch *Scheduler) reschedule(ctx context.Context, ichain IntervalChain) {
	if ichain.SelfDestruct {
		sch.pgengine.DeleteChainConfig(ctx, ichain.ChainExecutionConfigID)
		return
	}
	sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Sleeping before next execution for %ds for chain %s", ichain.Interval, ichain))
	select {
	case <-time.After(time.Duration(ichain.Interval) * time.Second):
		if sch.isValid(ichain) {
			sch.intervalChainsChan <- ichain
		}
	case <-ctx.Done():
		return
	}
}

func (sch *Scheduler) retrieveIntervalChainsAndRun(ctx context.Context) {
	sch.intervalChainMutex.Lock()
	ichains := []IntervalChain{}
	err := sch.pgengine.SelectIntervalChains(ctx, &ichains)
	if err != nil {
		sch.pgengine.LogToDB(ctx, "ERROR", "Could not query pending interval tasks: ", err)
	} else {
		sch.pgengine.LogToDB(ctx, "LOG", "Number of active interval chains: ", len(ichains))
	}

	// delete chains that are not returned from the database
	for id, ichain := range sch.intervalChains {
		if !ichain.isListed(ichains) {
			delete(sch.intervalChains, id)
		}
	}

	// update chains from the database and send to working channel new one
	for _, ichain := range ichains {
		if (IntervalChain{}) == sch.intervalChains[ichain.ChainExecutionConfigID] {
			sch.intervalChainsChan <- ichain
		}
		sch.intervalChains[ichain.ChainExecutionConfigID] = ichain
	}
	sch.intervalChainMutex.Unlock()
}

func (sch *Scheduler) intervalChainWorker(ctx context.Context, ichains <-chan IntervalChain) {
	for {
		select {
		case ichain := <-ichains:
			if !sch.isValid(ichain) { // chain not in the list of active chains
				continue
			}
			sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Calling process interval chain for %s", ichain))
			if !ichain.RepeatAfter {
				go sch.reschedule(ctx, ichain)
			}
			for !sch.pgengine.CanProceedChainExecution(ctx, ichain.ChainExecutionConfigID, ichain.MaxInstances) {
				sch.pgengine.LogToDB(ctx, "DEBUG", fmt.Sprintf("Cannot proceed with chain %s. Sleeping...", ichain))
				select {
				case <-time.After(time.Duration(pgengine.WaitTime) * time.Second):
				case <-ctx.Done():
					sch.pgengine.LogToDB(ctx, "ERROR", "request cancelled")
					return
				}
			}
			sch.Lock(ichain.ExclusiveExecution)
			sch.executeChain(ctx, ichain.ChainExecutionConfigID, ichain.ChainID)
			sch.Unlock(ichain.ExclusiveExecution)
			if ichain.RepeatAfter {
				go sch.reschedule(ctx, ichain)
			}
		case <-ctx.Done():
			return
		}
	}
}
