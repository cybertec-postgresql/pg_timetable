package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
)

// RestHandler is a common interface describing the current status of a connection
type RestHandler interface {
	IsReady() bool
	StartChain(context.Context, int) error
	StopChain(context.Context, int) error
}

type RestApiServer struct {
	ApiHandler RestHandler
	l          log.LoggerIface
	http.Server
}

func Init(opts config.RestApiOpts, logger log.LoggerIface) *RestApiServer {
	s := &RestApiServer{
		nil,
		logger,
		http.Server{
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
	http.HandleFunc("/startchain", s.chainHandler)
	http.HandleFunc("/stopchain", s.chainHandler)
	if opts.Port != 0 {
		logger.WithField("port", opts.Port).Info("Starting REST API server...")
		go func() { logger.Error(s.ListenAndServe()) }()
	}
	return s
}

func (Server *RestApiServer) readinessHandler(w http.ResponseWriter, r *http.Request) {
	Server.l.Debug("Received /readiness REST API request")
	if Server.ApiHandler == nil || !Server.ApiHandler.IsReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	r.Context()
}
