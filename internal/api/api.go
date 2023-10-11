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

type RestAPIServer struct {
	APIHandler RestHandler
	l          log.LoggerIface
	http.Server
}

func Init(opts config.RestAPIOpts, logger log.LoggerIface) *RestAPIServer {
	s := &RestAPIServer{
		nil,
		logger,
		http.Server{
			Addr:           fmt.Sprintf(":%d", opts.Port),
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
	}
	http.HandleFunc("/liveness", s.livenessHandler)
	http.HandleFunc("/readiness", s.readinessHandler)
	http.HandleFunc("/startchain", s.chainHandler)
	http.HandleFunc("/stopchain", s.chainHandler)
	if opts.Port != 0 {
		logger.WithField("port", opts.Port).Info("Starting REST API server...")
		go func() { logger.Error(s.ListenAndServe()) }()
	}
	return s
}

func (Server *RestAPIServer) livenessHandler(w http.ResponseWriter, _ *http.Request) {
	Server.l.Debug("Received /liveness REST API request")
	w.WriteHeader(http.StatusOK) // i'm serving hence I'm alive
}

func (Server *RestAPIServer) readinessHandler(w http.ResponseWriter, _ *http.Request) {
	Server.l.Debug("Received /readiness REST API request")
	if Server.APIHandler == nil || !Server.APIHandler.IsReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
