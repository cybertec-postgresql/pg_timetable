package client

import (
	"context"
	"fmt"
)

// StartChain sends a one-shot START notification to the named worker via
// timetable.notify_chain_start (REQ-005 / AC-002).
// The worker is REQUIRED (AC-002b); the caller must validate before calling.
// A non-zero delaySeconds sets the start delay passed to the function.
func (c *PgClient) StartChain(ctx context.Context, chainID int, worker string, delaySeconds int) error {
	if !c.workerExists(ctx, worker) {
		// Warn but still send: the worker may be temporarily disconnected or
		// starting up. The spec says SHOULD warn, not MUST block (§9).
		fmt.Printf("warning: worker %q not found in active_session; NOTIFY sent anyway\n", worker)
	}
	var delay interface{}
	if delaySeconds > 0 {
		delay = fmt.Sprintf("%d seconds", delaySeconds)
	}
	_, err := c.pool.Exec(ctx,
		`SELECT timetable.notify_chain_start($1, $2, $3::interval)`,
		chainID, worker, delay)
	return err
}

// StopChain sends a STOP notification to the named worker via
// timetable.notify_chain_stop (REQ-006 / AC-003).
// The worker is REQUIRED; the caller must validate before calling.
func (c *PgClient) StopChain(ctx context.Context, chainID int, worker string) error {
	if !c.workerExists(ctx, worker) {
		fmt.Printf("warning: worker %q not found in active_session; NOTIFY sent anyway\n", worker)
	}
	_, err := c.pool.Exec(ctx,
		`SELECT timetable.notify_chain_stop($1, $2)`,
		chainID, worker)
	return err
}

// PauseChain sets live=false on the named chain via timetable.pause_job
// (REQ-007 / AC-004).
func (c *PgClient) PauseChain(ctx context.Context, ref string) error {
	var ok bool
	if err := c.pool.QueryRow(ctx,
		`SELECT timetable.pause_job($1)`, ref).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("chain %q not found", ref)
	}
	return nil
}

// ResumeChain sets live=true on the named chain via timetable.resume_job
// (REQ-007).
func (c *PgClient) ResumeChain(ctx context.Context, ref string) error {
	var ok bool
	if err := c.pool.QueryRow(ctx,
		`SELECT timetable.resume_job($1)`, ref).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("chain %q not found", ref)
	}
	return nil
}

// workerExists reports whether a scheduler session with the given client_name
// is currently registered in timetable.active_session (P3-4 / §9).
func (c *PgClient) workerExists(ctx context.Context, worker string) bool {
	var exists bool
	_ = c.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM timetable.active_session WHERE client_name = $1)`,
		worker).Scan(&exists)
	return exists
}
