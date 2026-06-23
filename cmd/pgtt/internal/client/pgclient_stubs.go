package client

import (
	"context"
	"errors"
)

// errNotImplemented marks methods whose implementation lands in a later phase.
var errNotImplemented = errors.New("not implemented yet")

// --- Read & observe (implemented in Phase 2) ---

func (c *PgClient) ListChains(context.Context) ([]ChainListItem, error) {
	return nil, errNotImplemented
}

func (c *PgClient) ShowChain(context.Context, string) (*ChainListItem, []ChainTask, error) {
	return nil, nil, errNotImplemented
}

func (c *PgClient) ListSessions(context.Context) ([]Session, error) {
	return nil, errNotImplemented
}

func (c *PgClient) ListActiveChains(context.Context) ([]ActiveChain, error) {
	return nil, errNotImplemented
}

func (c *PgClient) ListLogs(context.Context, LogFilter) ([]LogEntry, error) {
	return nil, errNotImplemented
}

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
