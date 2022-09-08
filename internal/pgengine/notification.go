package pgengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pgconn "github.com/jackc/pgx/v5/pgconn"
)

// NotifyTTL specifies how long processed NOTIFY messages should be stored
var NotifyTTL int64 = 60

// ChainSignal used to hold asynchronous notifications from PostgreSQL server
type ChainSignal struct {
	ConfigID int    // chain configuration ifentifier
	Command  string // allowed: START, STOP
	Ts       int64  // timestamp NOTIFY sent
}

// Since there are usually multiple opened connections to the database, all of them will receive NOTIFY messages.
// To process each NOTIFY message only once we store each message with TTL 1 minute because the max idle period for a
// a connection is the main loop period of 1 minute.
var mutex sync.Mutex
var notifications map[ChainSignal]struct{} = func() (m map[ChainSignal]struct{}) {
	m = make(map[ChainSignal]struct{})
	go func() {
		for now := range time.Tick(time.Duration(NotifyTTL) * time.Second) {
			mutex.Lock()
			for k := range m {
				if now.Unix()-k.Ts > NotifyTTL {
					delete(m, k)
				}
			}
			mutex.Unlock()
		}
	}()
	return
}()

// NotificationHandler consumes notifications from the PostgreSQL server
func (pge *PgEngine) NotificationHandler(c *pgconn.PgConn, n *pgconn.Notification) {
	l := pge.l.WithField("pid", c.PID()).WithField("notification", *n)
	l.Debug("Notification received")
	var signal ChainSignal
	var err error
	if err = json.Unmarshal([]byte(n.Payload), &signal); err == nil {
		mutex.Lock()
		if _, ok := notifications[signal]; ok {
			l.WithField("handled", notifications).Debug("Notification already handled")
			mutex.Unlock()
			return
		}
		notifications[signal] = struct{}{}
		mutex.Unlock()
		switch signal.Command {
		case "STOP", "START":
			if signal.ConfigID > 0 {
				l.WithField("signal", signal).Info("Adding asynchronous chain to working queue")
				pge.chainSignalChan <- signal
				return
			}
		}
		err = fmt.Errorf("Unknown command: %s", signal.Command)
	}
	l.WithError(err).Error("Syntax error in payload")
}

// WaitForChainSignal returns configuration id from the notifications
func (pge *PgEngine) WaitForChainSignal(ctx context.Context) ChainSignal {
	select {
	case <-ctx.Done():
		return ChainSignal{0, "", 0}
	case signal := <-pge.chainSignalChan:
		return signal
	}
}

// HandleNotifications consumes notifications in blocking mode
func (pge *PgEngine) HandleNotifications(ctx context.Context) {
	conn, err := pge.ConfigDb.Acquire(ctx)
	if err != nil {
		pge.l.WithError(err).Error()
	}
	defer conn.Release()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		c := conn.Conn()
		if n, err := c.WaitForNotification(ctx); err == nil {
			pge.NotificationHandler(c.PgConn(), n)
		}
		if err != nil {
			pge.l.WithError(err).Error()
		}
	}
}
