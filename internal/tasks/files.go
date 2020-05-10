package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/cavaliercoder/grab"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

type downloadOpts struct {
	WorkersNum int      `json:"workersnum"`
	FileUrls   []string `json:"fileurls"`
	DestPath   string   `json:"destpath"`
}

func taskDownloadFile(paramValues string) error {
	var opts downloadOpts
	if err := json.Unmarshal([]byte(paramValues), &opts); err != nil {
		return err
	}
	if len(opts.FileUrls) == 0 {
		return errors.New("Files to download are not specified")
	}
	if _, err := os.Stat(opts.DestPath); err != nil {
		return err
	}
	return downloadUrls(opts.FileUrls, opts.DestPath, opts.WorkersNum)
}

// downloadUrls function implemented using grab library
func downloadUrls(urls []string, dest string, workers int) error {
	// create multiple download requests
	reqs := make([]*grab.Request, 0)
	for _, url := range urls {
		req, err := grab.NewRequest(dest, url)
		if err != nil {
			return err
		}
		reqs = append(reqs, req)
	}
	// start downloads with workers, if WorkersNum <= 0, then worker for each file
	client := grab.NewClient()
	respch := client.DoBatch(workers, reqs...)
	// check each response
	var errstrings []string
	for resp := range respch {
		if err := resp.Err(); err != nil {
			errstrings = append(errstrings, err.Error())
		} else {
			pgengine.LogToDB("LOG", fmt.Sprintf("Downloaded %s to %s", resp.Request.URL(), resp.Filename))
		}
	}
	if len(errstrings) > 0 {
		return fmt.Errorf("download failed: %v", errstrings)
	}
	return nil
}
