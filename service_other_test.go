//go:build !windows

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_handleServiceCommand(t *testing.T) {
	assert.Equal(t, handleServiceCommand(nil), ExitCodeFatalError)

}
