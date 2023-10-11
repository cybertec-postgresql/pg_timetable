package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/api"
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

var restsrv *api.RestAPIServer

func init() {
	restsrv = api.Init(config.RestAPIOpts{Port: 8080}, log.Init(config.LoggingOpts{LogLevel: "error"}))
}

func TestStatus(t *testing.T) {

	r, err := http.Get("http://localhost:8080/liveness")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, r.StatusCode)

	r, err = http.Get("http://localhost:8080/readiness")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, r.StatusCode)

	restsrv.APIHandler = &apihandler{}
	r, err = http.Get("http://localhost:8080/readiness")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, r.StatusCode)
}

func TestChainManager(t *testing.T) {
	restsrv.APIHandler = &apihandler{}
	r, err := http.Get("http://localhost:8080/startchain")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, r.StatusCode)
	b, _ := io.ReadAll(r.Body)
	assert.Contains(t, string(b), "invalid syntax")

	r, err = http.Get("http://localhost:8080/startchain?id=1")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, r.StatusCode)

	r, err = http.Get("http://localhost:8080/stopchain?id=1")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, r.StatusCode)

	r, err = http.Get("http://localhost:8080/startchain?id=0")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, r.StatusCode)
	b, _ = io.ReadAll(r.Body)
	assert.Contains(t, string(b), "invalid chain id")
}
