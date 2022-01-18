package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
)

// StatusIface is a common interface describing the current status of a connection
type StatusIface interface {
	IsReady() bool
}

type RestApiServer struct {
	StatusReporter StatusIface
	l              log.LoggerIface
	*http.Server
}

func Init(opts config.RestApiOpts, logger log.LoggerIface) *RestApiServer {
	s := &RestApiServer{
		nil,
		logger,
		&http.Server{
			Addr:           fmt.Sprintf(":%d", opts.Port),
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
	}
	http.HandleFunc("/liveness", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // i'm serving hence I'm alive
	})
	http.HandleFunc("/readiness", s.readinessHandler)
	if opts.Port != 0 {
		logger.WithField("port", opts.Port).Info("Starting REST API server...")
		go func() { logger.Error(s.ListenAndServe()) }()
	}
	return s
}

func (Server *RestApiServer) readinessHandler(w http.ResponseWriter, r *http.Request) {
	Server.l.Debug("Received /readiness REST API request")
	if Server.StatusReporter == nil || !Server.StatusReporter.IsReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
