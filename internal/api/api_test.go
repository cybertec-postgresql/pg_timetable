package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/stretchr/testify/assert"
)

type apihandler struct {
}

func (r *apihandler) IsReady() bool {
	return true
}

func (r *apihandler) StartChain(_ context.Context, chainID int) error {
	if chainID == 0 {
		return errors.New("invalid chain id")
	}
	return nil
}

func (r *apihandler) StopChain(context.Context, int) error {
	return nil
}

var restsrv = Init(config.RestAPIOpts{Port: 8080}, log.Init(config.LoggingOpts{LogLevel: "error"}))

const turl = "http://localhost:8080/"

func TestStatus(t *testing.T) {
	assert.HTTPSuccess(t, restsrv.livenessHandler, "GET", turl+"liveness", nil)
	assert.HTTPStatusCode(t, restsrv.readinessHandler, "GET", turl+"readiness", nil, http.StatusServiceUnavailable)

	restsrv.APIHandler = &apihandler{}
	assert.HTTPSuccess(t, restsrv.readinessHandler, "GET", turl+"readiness", nil)
}

func TestChainManager(t *testing.T) {
	restsrv.APIHandler = &apihandler{}
	assert.HTTPStatusCode(t, restsrv.chainHandler, "GET", turl+"startchain", nil, http.StatusBadRequest)
	assert.HTTPBodyContains(t, restsrv.chainHandler, "GET", turl+"startchain", nil, "invalid syntax")

	assert.HTTPSuccess(t, restsrv.chainHandler, "GET", turl+"startchain",
		url.Values{"id": []string{"1"}})
	assert.HTTPSuccess(t, restsrv.chainHandler, "GET", turl+"stopchain",
		url.Values{"id": []string{"1"}})

	assert.HTTPError(t, restsrv.chainHandler, "GET", turl+"startchain",
		url.Values{"id": []string{"0"}})
	assert.HTTPBodyContains(t, restsrv.chainHandler, "GET", turl+"startchain",
		url.Values{"id": []string{"0"}}, "invalid chain id")
}
