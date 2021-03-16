package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

const workersNumber = 16

//the main loop period. Should be 60 (sec) for release configuration. Set to 10 (sec) for debug purposes
const refetchTimeout = 60

// if the number of chains pulled for execution is higher than this value, try to spread execution to avoid spikes
const maxChainsThreshold = workersNumber * refetchTimeout

// RunStatus specifies the current status of execution
type RunStatus int

const (
	// ConnectionDroppped specifies the connection has been dropped
	ConnectionDroppped RunStatus = iota
	// ContextCancelled specifies the context has been cancelled probably due to timeout
	ContextCancelled
)

type Scheduler struct {
	chains        chan Chain // channel for passing chains to workers
	pgengine      *pgengine.PgEngine
	WorkersNumber int

	exclusiveMutex sync.RWMutex //read-write mutex for running regular and exclusive chains

	// activeChains holds the map of chain ID with context cancel() function, so we can abort chain by request
	activeChains     map[int]func()
	activeChainMutex sync.Mutex

	// map of active chains, updated every minute
	intervalChains map[int]IntervalChain
	// create channel for passing interval chains to workers
	intervalChainsChan chan IntervalChain
	intervalChainMutex sync.Mutex
}

func New(pge *pgengine.PgEngine) *Scheduler {
	return &Scheduler{
		WorkersNumber:      workersNumber,
		chains:             make(chan Chain, workersNumber),
		pgengine:           pge,
		activeChains:       make(map[int]func()),
		intervalChains:     make(map[int]IntervalChain),
		intervalChainsChan: make(chan IntervalChain, workersNumber),
	}
}

//Run executes jobs. Returns Fa
func (sch *Scheduler) Run(ctx context.Context, debug bool) RunStatus {
	// create sleeping workers waiting data on channel
	for w := 1; w <= workersNumber; w++ {
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go sch.chainWorker(workerCtx, sch.chains)
		workerCtx, cancel = context.WithCancel(ctx)
		defer cancel()
		go sch.intervalChainWorker(workerCtx, sch.intervalChainsChan)
	}
	/* set maximum connection to workersNumber + 1 for system calls */
	//pgengine.ConfigDb.SetMaxOpenConns(workersNumber)
	/* cleanup potential database leftovers */
	sch.pgengine.FixSchedulerCrash(ctx)

	/*
		Loop forever or until we ask it to stop.
		First loop fetches notifications.
		Main loop works every refetchTimeout seconds and runs chains.
	*/
	sch.pgengine.LogToDB(ctx, "LOG", "Accepting asynchronous chains execution requests...")
	go sch.retrieveAsyncChainsAndRun(ctx)

	if debug { //run blocking notifications receiving
		sch.pgengine.HandleNotifications(ctx)
		return ContextCancelled
	}

	sch.pgengine.LogToDB(ctx, "LOG", "Checking for @reboot task chains...")
	sch.retrieveChainsAndRun(ctx, true)

	for {
		sch.pgengine.LogToDB(ctx, "LOG", "Checking for task chains...")
		go sch.retrieveChainsAndRun(ctx, false)
		sch.pgengine.LogToDB(ctx, "LOG", "Checking for interval task chains...")
		go sch.retrieveIntervalChainsAndRun(ctx)

		select {
		case <-time.After(refetchTimeout * time.Second):
			// pass
		case <-ctx.Done():
			return ContextCancelled
		}
	}
}
