package pgengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pgconn "github.com/jackc/pgconn"
	stdlib "github.com/jackc/pgx/v4/stdlib"
)

// NotifyTTL specifies how long processed NOTIFY messages should be stored
var NotifyTTL int64 = 60

type ChainSignal struct {
	ConfigID int    // chain configuration ifentifier
	Command  string // allowed: START, STOP
	Ts       int64  // timestamp NOTIFY sent
}

// NOTIFY messages passed verification are pushed to this channel
var chainSignalChan chan ChainSignal = make(chan ChainSignal, 64)

//  Since there are usually multiple opened connections to the database, all of them will receive NOTIFY messages.
//  To process each NOTIFY message only once we store each message with TTL 1 minute because the max idle period for a
//  a connection is the main loop period of 1 minute.
var mutex sync.Mutex
var notifications map[ChainSignal]struct{} = func() (m map[ChainSignal]struct{}) {
	m = make(map[ChainSignal]struct{})
	go func() {
		for now := range time.Tick(time.Duration(NotifyTTL) * time.Minute) {
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
func NotificationHandler(c *pgconn.PgConn, n *pgconn.Notification) {
	Log("DEBUG", "Notification received: ", *n, " Connection PID: ", c.PID())
	var signal ChainSignal
	var err error
	if err = json.Unmarshal([]byte(n.Payload), &signal); err == nil {
		mutex.Lock()
		if _, ok := notifications[signal]; ok {
			return // already handled
		}
		notifications[signal] = struct{}{}
		mutex.Unlock()
		switch signal.Command {
		case "STOP", "START":
			if signal.ConfigID > 0 {
				Log("DEBUG", "Adding asynchronous chain to working queue: ", signal)
				chainSignalChan <- signal
				return
			}
		}
		err = fmt.Errorf("Unknown command: %s", signal.Command)
	}
	Log("ERROR", "Syntax error in payload: ", err)
}

// WaitForChainSignal returns configuration id from the notifications
func WaitForChainSignal(ctx context.Context) ChainSignal {
	select {
	case <-ctx.Done():
		return ChainSignal{0, "", 0}
	case signal := <-chainSignalChan:
		return signal
	}
}

// HandleNotifications consumes notifications in blocking mode
func HandleNotifications(ctx context.Context) {
	conn, err := ConfigDb.DB.Conn(ctx)
	if err != nil {
		LogToDB(ctx, "ERROR", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err = conn.Raw(func(driverConn interface{}) error {
			c := driverConn.(*stdlib.Conn).Conn()
			if n, err := c.WaitForNotification(ctx); err == nil {
				NotificationHandler(c.PgConn(), n) // remove processed flag in one threaded debug mode
			}
			return err
		})
		if err != nil {
			LogToDB(ctx, "ERROR", err)
		}
	}
}
