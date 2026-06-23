package client

import (
	"context"
	"errors"
)

// errNotImplemented marks methods whose implementation lands in a later phase.
var errNotImplemented = errors.New("not implemented yet")

// --- Live control (implemented in Phase 3) ---

func (c *PgClient) StartChain(context.Context, int, string, int) error {
	return errNotImplemented
}

func (c *PgClient) StopChain(context.Context, int, string) error {
	return errNotImplemented
}

func (c *PgClient) PauseChain(context.Context, string) error {
	return errNotImplemented
}

func (c *PgClient) ResumeChain(context.Context, string) error {
	return errNotImplemented
}

// compile-time assertion that PgClient satisfies Client.
var _ Client = (*PgClient)(nil)
