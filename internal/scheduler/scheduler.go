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
	// RunningStatus specifies the scheduler is in the main loop processing chains
	RunningStatus RunStatus = iota
	// ContextCancelledStatus specifies the context has been cancelled probably due to timeout
	ContextCancelledStatus
	// Shutdown specifies proper termination of the session
	ShutdownStatus
)

// Scheduler is the main class for running the tasks
type Scheduler struct {
	pgengine    *pgengine.PgEngine
	l           log.LoggerIface
	chainsChan  chan Chain         // channel for passing chains to workers
	ichainsChan chan IntervalChain // channel for passing interval chains to workers

	exclusiveMutex sync.RWMutex //read-write mutex for running regular and exclusive chains

	activeChains     map[int]func() // map of chain ID with context cancel() function to abort chain by request
	activeChainMutex sync.Mutex

	intervalChains     map[int]IntervalChain // map of active chains, updated every minute
	intervalChainMutex sync.Mutex

	shutdown chan struct{} // closed when shutdown is called
	status   RunStatus
}

// Max returns the maximum number of two arguments
func Max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

// New returns a new instance of Scheduler
func New(pge *pgengine.PgEngine, logger log.LoggerIface) *Scheduler {
	return &Scheduler{
		l:              logger,
		pgengine:       pge,
		chainsChan:     make(chan Chain, Max(minChannelCapacity, pge.Resource.CronWorkers*2)),
		ichainsChan:    make(chan IntervalChain, Max(minChannelCapacity, pge.Resource.IntervalWorkers*2)),
		activeChains:   make(map[int]func()), //holds cancel() functions to stop chains
		intervalChains: make(map[int]IntervalChain),
		shutdown:       make(chan struct{}),
		status:         RunningStatus,
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

func (sch *Scheduler) StartChain(ctx context.Context, chainId int) error {
	return sch.processAsyncChain(ctx, ChainSignal{
		ConfigID: chainId,
		Command:  "START",
		Ts:       time.Now().Unix()})
}

func (sch *Scheduler) StopChain(ctx context.Context, chainId int) error {
	return sch.processAsyncChain(ctx, ChainSignal{
		ConfigID: chainId,
		Command:  "STOP",
		Ts:       time.Now().Unix()})
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
		go sch.intervalChainWorker(workerCtx, sch.ichainsChan)
	}
	ctx = log.WithLogger(ctx, sch.l)

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
