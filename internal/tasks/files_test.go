package tasks

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if filenamep := r.URL.Query().Get("filename"); filenamep == "" {
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=\"%s\"", "test.txt"))
	bw := bufio.NewWriterSize(w, 4096)
	for i := 0; i < 4096; i++ {
		_, _ = bw.Write([]byte{byte(i)})
	}
	bw.Flush()
	w.WriteHeader(http.StatusOK)
}))

func TestDownloadFile(t *testing.T) {
	assert.EqualError(t, taskDownloadFile(""), `unexpected end of JSON input`,
		"Download with empty param should fail")
	assert.EqualError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": [] }`),
		"Files to download are not specified", "Download with empty files should fail")
	assert.Error(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "non-existent" }`),
		"Downlod with non-existent directory or insufficient rights should fail")
	assert.Error(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["`+ts.URL+`"], "destpath": "." }`),
		"Downlod with incorrect url should fail")
	assert.NoError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["`+ts.URL+`?filename=test.txt"], "destpath": "." }`),
		"Downlod with correct json input should succeed")
	assert.NoError(t, os.RemoveAll("test.txt"), "Test output should be removed")

	assert.Error(t, downloadUrls([]string{"\t"}, "", 1), "Downlod with incorrect URL should fail")
}
