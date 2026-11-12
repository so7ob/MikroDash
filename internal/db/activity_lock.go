package db

import (
	"context"
	"sync"
)

// activityMutex serializes projection snapshots with import transactions. A
// canceled HTTP query does not wait behind unrelated work before returning.
type activityMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *activityMutex) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.once.Do(func() { m.token = make(chan struct{}, 1) })
	select {
	case m.token <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *activityMutex) Lock()   { _ = m.LockContext(context.Background()) }
func (m *activityMutex) Unlock() { <-m.token }
