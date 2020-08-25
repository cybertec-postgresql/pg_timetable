package scheduler

import (
	"context"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v4/stdlib"
)

const workersNumber = 16

//the main loop period. Should be 60 (sec) for release configuration. Set to 10 (sec) for debug purposes
const refetchTimeout = 60

// if the number of chains pulled for execution is higher than this value, try to spread execution to avoid spikes
const maxChainsThreshold = workersNumber * refetchTimeout

// create channel for passing chains to workers
var chains chan Chain = make(chan Chain, workersNumber)

// RunStatus specifies the current status of execution
type RunStatus int

const (
	// ConnectionDroppped specifies the connection has been dropped
	ConnectionDroppped RunStatus = iota
	// ContextCancelled specifies the context has been cancelled probably due to timeout
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
