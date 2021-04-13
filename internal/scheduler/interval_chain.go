package scheduler

import (
	"context"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
)

// IntervalChain structure used to represent repeated chains.
type IntervalChain struct {
	Chain
	Interval    int  `db:"interval_seconds"`
	RepeatAfter bool `db:"repeat_after"`
}

func (ichain IntervalChain) isListed(ichains []IntervalChain) bool {
	for _, ic := range ichains {
		if ichain.ChainID == ic.ChainID {
			return true
		}
	}
	return false
}

func (sch *Scheduler) isValid(ichain IntervalChain) bool {
	return (IntervalChain{}) != sch.intervalChains[ichain.ChainID]
}

func (sch *Scheduler) reschedule(ctx context.Context, ichain IntervalChain) {
	if ichain.SelfDestruct {
		sch.pgengine.DeleteChainConfig(ctx, ichain.ChainID)
		return
	}
	log.GetLogger(ctx).Debug("Sleeping before next execution of interval chain")
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
		sch.l.WithError(err).Error("Could not query pending interval tasks")
	} else {
		sch.l.WithField("count", len(ichains)).Info("Retrieve interval chains to run")
	}

	// delete chains that are not returned from the database
	for id, ichain := range sch.intervalChains {
		if !ichain.isListed(ichains) {
			delete(sch.intervalChains, id)
		}
	}

	// update chains from the database and send to working channel new one
	for _, ichain := range ichains {
		if (IntervalChain{}) == sch.intervalChains[ichain.ChainID] {
			sch.intervalChainsChan <- ichain
		}
		sch.intervalChains[ichain.ChainID] = ichain
	}
	sch.intervalChainMutex.Unlock()
}

func (sch *Scheduler) intervalChainWorker(ctx context.Context, ichains <-chan IntervalChain) {
	for {
		select {
		case <-ctx.Done(): //check context with high priority
			return
		default:
			select {
			case ichain := <-ichains:
				if !sch.isValid(ichain) { // chain not in the list of active chains
					continue
				}
				chainL := sch.l.WithField("chain", ichain.ChainID)
				chainContext := log.WithLogger(ctx, chainL)
				chainL.Info("Starting chain")
				if !ichain.RepeatAfter {
					go sch.reschedule(chainContext, ichain)
				}
				if !sch.pgengine.CanProceedChainExecution(chainContext, ichain.ChainID, ichain.MaxInstances) {
					chainL.Debug("Cannot proceed. Sleeping")
					if ichain.RepeatAfter {
						go sch.reschedule(chainContext, ichain)
					}
					continue
				}
				sch.Lock(ichain.ExclusiveExecution)
				sch.executeChain(chainContext, ichain.ChainID, ichain.TaskID)
				sch.Unlock(ichain.ExclusiveExecution)
				if ichain.RepeatAfter {
					go sch.reschedule(chainContext, ichain)
				}
			case <-ctx.Done():
				return
			}
		}
	}
}
