package scheduler

import (
	"fmt"
	"sync"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

//Select live chains with proper client_name value
const sqlSelectIntervalChains = `
SELECT
	chain_execution_config, chain_id, chain_name, self_destruct, exclusive_execution, COALESCE(max_instances, 16) as max_instances,
	EXTRACT(EPOCH FROM (substr(run_at, 7) :: interval)) :: int4 as interval_seconds,
	starts_with(run_at, '@after') as repeat_after
FROM 
	timetable.chain_execution_config 
WHERE 
	live AND (client_name = $1 or client_name IS NULL) AND substr(run_at, 1, 6) IN ('@every', '@after')`

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

func (ichain IntervalChain) isValid() bool {
	return (IntervalChain{}) != intervalChains[ichain.ChainExecutionConfigID]
}

// map of active chains, updated every minute
var intervalChains map[int]IntervalChain = make(map[int]IntervalChain)

// create channel for passing interval chains to workers
var intervalChainsChan chan IntervalChain = make(chan IntervalChain)

var mutex = &sync.Mutex{}

func retriveIntervalChainsAndRun(sql string) {
	mutex.Lock()
	ichains := []IntervalChain{}
	err := pgengine.ConfigDb.Select(&ichains, sql, pgengine.ClientName)
	if err != nil {
		pgengine.LogToDB("ERROR", "Could not query pending interval tasks: ", err)
	} else {
		pgengine.LogToDB("LOG", "Number of active interval chains: ", len(ichains))
	}

	// delete chains that are not returned from the database
	for id, ichain := range intervalChains {
		if !ichain.isListed(ichains) {
			delete(intervalChains, id)
		}
	}

	// update chains from the database and send to working channel new one
	for _, ichain := range ichains {
		if (IntervalChain{}) == intervalChains[ichain.ChainExecutionConfigID] {
			intervalChainsChan <- ichain
		}
		intervalChains[ichain.ChainExecutionConfigID] = ichain
	}
	mutex.Unlock()
}

func intervalChainWorker(ichains <-chan IntervalChain) {

	for ichain := range ichains {
		pgengine.LogToDB("DEBUG", fmt.Sprintf("Calling process interval chain for %s", ichain))

		if !ichain.isValid() { // chain not in the list of active chains
			return
		}

		if !ichain.RepeatAfter {
			go func() {
				pgengine.LogToDB("DEBUG", fmt.Sprintf("Sleeping before next execution in %ds for chain %s", ichain.Interval, ichain))
				time.Sleep(time.Duration(ichain.Interval) * time.Second)
				if ichain.isValid() {
					intervalChainsChan <- ichain
				}
			}()
		}

		if !pgengine.CanProceedChainExecution(ichain.ChainExecutionConfigID, ichain.MaxInstances) {
			pgengine.LogToDB("DEBUG", fmt.Sprintf("Cannot proceed with chain %s. Skipping...", ichain))
			return
		}

		executeChain(ichain.ChainExecutionConfigID, ichain.ChainID)
		if ichain.SelfDestruct {
			pgengine.DeleteChainConfig(ichain.ChainExecutionConfigID)
		} else if ichain.RepeatAfter {
			go func() {
				pgengine.LogToDB("DEBUG", fmt.Sprintf("Sleeping before next execution in %ds for chain %s", ichain.Interval, ichain))
				time.Sleep(time.Duration(ichain.Interval) * time.Second)
				if ichain.isValid() {
					intervalChainsChan <- ichain
				}
			}()
		}
	}
}
