package pgengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	pgconn "github.com/jackc/pgconn"
	stdlib "github.com/jackc/pgx/v4/stdlib"
)

type ChainSignal struct {
	ConfigID int
	Command  string //allowed: START, STOP
}

var notifications map[pgconn.Notification]struct{} = make(map[pgconn.Notification]struct{})
var chainSignalChan chan ChainSignal = make(chan ChainSignal, 64)
var mutex sync.Mutex

// NotificationHandler consumes notifications from the PostgreSQL server
func NotificationHandler(c *pgconn.PgConn, n *pgconn.Notification) {
	mutex.Lock()
	defer mutex.Unlock()
	Log("DEBUG", "Notification received: ", *n, " Connection PID: ", c.PID())
	if _, ok := notifications[*n]; ok {
		return // already handled
	}
	notifications[*n] = struct{}{}
	var signal ChainSignal
	var err error
	if err = json.Unmarshal([]byte(n.Payload), &signal); err == nil {
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
		return ChainSignal{0, ""}
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
				NotificationHandler(c.PgConn(), n)
			}
			return err
		})
		if err != nil {
			LogToDB(ctx, "ERROR", err)
		}
	}
}
