package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

// the main loop period. Should be 60 (sec) for release configuration. Set to 10 (sec) for debug purposes
const refetchTimeout = 60

// the min capacity of chains channels
const minChannelCapacity = 1024

// RunStatus specifies the current status of execution
type RunStatus int

const (
	// RunningStatus specifies the connection has been dropped
	RunningStatus RunStatus = iota
	// ContextCancelledStatus specifies the context has been cancelled probably due to timeout
	ContextCancelledStatus
	// Shutdown specifies proper termination of the session
	ShutdownStatus
)

// Scheduler is the main class for running the tasks
type Scheduler struct {
	l          log.LoggerIface
	chainsChan chan Chain // channel for passing chains to workers
	pgengine   *pgengine.PgEngine

	exclusiveMutex sync.RWMutex //read-write mutex for running regular and exclusive chains

	// activeChains holds the map of chain ID with context cancel() function, so we can abort chain by request
	activeChains     map[int]func()
	activeChainMutex sync.Mutex

	// map of active chains, updated every minute
	intervalChains map[int]IntervalChain
	// create channel for passing interval chains to workers
	intervalChainsChan chan IntervalChain
	intervalChainMutex sync.Mutex
	shutdown           chan struct{} // closed when shutdown is called
	status             RunStatus
}

func max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

// New returns a new instance of Scheduler
func New(pge *pgengine.PgEngine, logger log.LoggerIface) *Scheduler {
	return &Scheduler{
		l:                  logger,
		pgengine:           pge,
		chainsChan:         make(chan Chain, max(minChannelCapacity, pge.Resource.CronWorkers*2)),
		intervalChainsChan: make(chan IntervalChain, max(minChannelCapacity, pge.Resource.IntervalWorkers*2)),
		activeChains:       make(map[int]func()), //holds cancel() functions to stop chains
		intervalChains:     make(map[int]IntervalChain),
		shutdown:           make(chan struct{}),
		status:             RunningStatus,
	}
}

// Shutdown terminates the current session
func (sch *Scheduler) Shutdown() {
	close(sch.shutdown)
}

// Config returns the current configuration for application
func (sch *Scheduler) Config() config.CmdOptions {
	return sch.pgengine.CmdOptions
}

// IsReady returns True if the scheduler is in the main loop processing chains
func (sch *Scheduler) IsReady() bool {
	return sch.status == RunningStatus
}

// Run executes jobs. Returns RunStatus why it terminated.
// There are only two possibilities: dropped connection and cancelled context.
func (sch *Scheduler) Run(ctx context.Context) RunStatus {
	// create sleeping workers waiting data on channel
	for w := 1; w <= sch.Config().Resource.CronWorkers; w++ {
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go sch.chainWorker(workerCtx, sch.chainsChan)
	}
	for w := 1; w <= sch.Config().Resource.IntervalWorkers; w++ {
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go sch.intervalChainWorker(workerCtx, sch.intervalChainsChan)
	}
	ctx = log.WithLogger(ctx, sch.l)
	/* cleanup potential database leftovers */
	sch.pgengine.FixSchedulerCrash(ctx)

	/*
		Loop forever or until we ask it to stop.
		First loop fetches notifications.
		Main loop works every refetchTimeout seconds and runs chains.
	*/
	sch.l.Info("Accepting asynchronous chains execution requests...")
	go sch.retrieveAsyncChainsAndRun(ctx)

	if sch.Config().Start.Debug { //run blocking notifications receiving
		sch.pgengine.HandleNotifications(ctx)
		return ContextCancelledStatus
	}

	sch.l.Debug("Checking for @reboot task chains...")
	sch.retrieveChainsAndRun(ctx, true)

	for {
		sch.l.Debug("Checking for task chains...")
		go sch.retrieveChainsAndRun(ctx, false)
		sch.l.Debug("Checking for interval task chains...")
		go sch.retrieveIntervalChainsAndRun(ctx)

		select {
		case <-time.After(refetchTimeout * time.Second):
			// pass
		case <-ctx.Done():
			sch.status = ContextCancelledStatus
		case <-sch.shutdown:
			sch.status = ShutdownStatus
			sch.terminateChains()
		}

		if sch.status != RunningStatus {
			return sch.status
		}
	}
}
