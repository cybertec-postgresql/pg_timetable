package pgtt_http

import (
	"fmt"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"net/http"
	"sync"
	"time"
)

var httpStatus = "starting"
var httpStatusMutex sync.RWMutex

func setHttpStatus(value string) {
	httpStatusMutex.Lock()
	defer httpStatusMutex.Unlock()
	httpStatus = value
}

func SetHttpStatusRunning() {
	setHttpStatus("running")
}

func StartHTTP(logger log.LoggerHookerIface, portNumber int) {
	s := &http.Server{
		Addr: fmt.Sprintf(":%d", portNumber),
		//Handler:        httpHandler,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	http.HandleFunc("/status", readyHandler)
	logger.Infof("Starting HTTP server on port %d...", portNumber)
	err := s.ListenAndServe()
	if err != nil {
		logger.WithError(err).Error("Error start HTTP server")
	}
	//logger.Printf("Started HTTP server on port %d", portNumber)
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	httpStatusMutex.RLock()
	defer httpStatusMutex.RUnlock()
	fmt.Fprint(w, httpStatus)
	//fmt.Fprintf(w, "ready, %q", html.EscapeString(r.URL.Path))
}
