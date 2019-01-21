package scheduler

import (
	"log"
	"os/exec"
)

func checkExeExists(exe string, msg string) {
	if _, err := exec.LookPath(exe); err != nil {
		log.Fatalln(msg, err)
	}
}
