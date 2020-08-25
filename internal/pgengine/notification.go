package pgengine

import (
	"context"
	"strconv"
	"sync"

	pgconn "github.com/jackc/pgconn"
	stdlib "github.com/jackc/pgx/v4/stdlib"
)

var notifications map[pgconn.Notification]struct{} = make(map[pgconn.Notification]struct{})
var configIDsChan chan int = make(chan int, 64)
var mutex = &sync.Mutex{}

// NotificationHandler consumes notifications from the PostgreSQL server
func NotificationHandler(c *pgconn.PgConn, n *pgconn.Notification) {
	mutex.Lock()
	if _, ok := notifications[*n]; ok {
		return // already handled
	}
	notifications[*n] = struct{}{}
	if id, err := strconv.Atoi(n.Payload); err == nil {
		configIDsChan <- id
	}
	mutex.Unlock()
}

// WaitForAsyncChain returns configuration id from the notifications
func WaitForAsyncChain(ctx context.Context) int {
	select {
	case <-ctx.Done():
		return 0
	case id := <-configIDsChan:
		return id
	}
}

// HandleNotifications consumes notifications in blocking mode
func HandleNotifications(ctx context.Context) {
	conn, err := ConfigDb.DB.Conn(ctx)
	if err != nil {
		LogToDB(ctx, "ERROR", err)
	}
	_, err = conn.ExecContext(ctx, "LISTEN "+ClientName)
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
