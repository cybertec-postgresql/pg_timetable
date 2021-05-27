package tasks

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if filenamep := r.URL.Query().Get("filename"); filenamep != "test.txt" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=\"%s\"", "test.txt"))
	bw := bufio.NewWriterSize(w, 4096)
	for i := 0; i < 4096; i++ {
		_, _ = bw.Write([]byte{byte(i)})
	}
	_ = bw.Flush()
	w.WriteHeader(http.StatusOK)
}))

func TestDownloadFile(t *testing.T) {
	ctx := context.Background()
	_, err := DownloadUrls(ctx, []string{ts.URL + `?filename=nonexistent.txt`}, "", 0)
	assert.Error(t, err, "Downlod with non-existent directory or insufficient rights should fail")
	_, err = DownloadUrls(ctx, []string{ts.URL + `?filename=test.txt`}, ".", 0)
	assert.NoError(t, err, "Downlod with correct json input should succeed")
	assert.NoError(t, os.RemoveAll("test.txt"), "Test output should be removed")
	_, err = DownloadUrls(ctx, []string{"\t"}, "", 1)
	assert.Error(t, err, "Download with incorrect URL should fail")
}
