package api_test

import (
	"net/http"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/api"
	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/stretchr/testify/assert"
)

type reporter struct {
}

func (r *reporter) IsReady() bool {
	return true
}

func TestStatus(t *testing.T) {
	restsrv := api.Init(config.RestApiOpts{Port: 8080}, log.Init(config.LoggingOpts{LogLevel: "error"}))
	r, err := http.Get("http://localhost:8080/liveness")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, r.StatusCode)

	r, err = http.Get("http://localhost:8080/readiness")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, r.StatusCode)

	restsrv.Reporter = &reporter{}
	r, err = http.Get("http://localhost:8080/readiness")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, r.StatusCode)
}
