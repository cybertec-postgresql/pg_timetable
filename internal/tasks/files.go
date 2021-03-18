package tasks

import (
	"context"
	"fmt"

	"github.com/cavaliercoder/grab"
)

// DownloadUrls function implemented using grab library
func DownloadUrls(ctx context.Context, urls []string, dest string, workers int) (out string, err error) {
	var req *grab.Request
	// create multiple download requests
	reqs := make([]*grab.Request, 0)
	for _, url := range urls {
		req, err = grab.NewRequest(dest, url)
		if err != nil {
			return
		}
		req = req.WithContext(ctx)
		reqs = append(reqs, req)
	}
	// start downloads with workers, if WorkersNum <= 0, then worker for each file
	client := grab.NewClient()
	respch := client.DoBatch(workers, reqs...)
	// check each response
	var errstrings []string
	for resp := range respch {
		if err = resp.Err(); err != nil {
			errstrings = append(errstrings, err.Error())
		} else {
			out = out + fmt.Sprintf("Downloaded %s to %s\n", resp.Request.URL(), resp.Filename)
		}
	}
	if len(errstrings) > 0 {
		err = fmt.Errorf("download failed: %v", errstrings)
	}
	return
}
