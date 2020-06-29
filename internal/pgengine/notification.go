package pgengine

import (
	"context"
	"strconv"

	pgconn "github.com/jackc/pgconn"
	stdlib "github.com/jackc/pgx/v4/stdlib"
)

var notifications map[pgconn.Notification]struct{} = make(map[pgconn.Notification]struct{})
var configIDsChan chan int = make(chan int)

func notificationHandler(c *pgconn.PgConn, n *pgconn.Notification) {
	if _, ok := notifications[*n]; ok {
		return // already handled
	}
	notifications[*n] = struct{}{}
	LogToDB(context.Background(), "DEBUG", "Async notifications received: ", len(notifications))
	if id, err := strconv.Atoi(n.Payload); err == nil {
		configIDsChan <- id
		LogToDB(context.Background(), "LOG", "Received async execution request: ", *n)
	}
}

func WaitForAsyncChain(ctx context.Context) int {
	select {
	case <-ctx.Done():
		return 0
	case id := <-configIDsChan:
		return id
	}
}

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
				notificationHandler(c.PgConn(), n)
			}
			return nil
		})
		if err != nil {
			LogToDB(ctx, "ERROR", err)
		}
	}
}
