package activitywire

import (
	"context"
	"log"
	"time"
)

// Local maintenance never opens a RouterOS channel. Durable batches resume after
// cancellation or restart; all legacy history stays readable until completion.
func (w *Wire) indexLoop(ctx context.Context) {
	defer close(w.indexDone)
	previous, _ := w.db.ActivityIndexes(ctx)
	lastNotice := time.Now()
	delay := time.NewTimer(0)
	defer delay.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-delay.C:
		}
		n, err := w.db.IndexActivityBatch(ctx, 500)
		if err != nil && ctx.Err() == nil {
			log.Printf("[activity] local index batch failed; will retry")
		}
		if n < 500 || time.Since(lastNotice) > time.Second {
			lastNotice = time.Now()
			states, e := w.db.ActivityIndexes(ctx)
			if e == nil {
				for id, state := range states {
					old, known := previous[id]
					if known && (old.Pending > 0 && state.Pending == 0) && w.changed != nil {
						w.changed(id)
					}
				}
				previous = states
			}
		}
		wait := 5 * time.Second
		if err == nil && n > 0 {
			wait = 10 * time.Millisecond
		}
		delay.Reset(wait)
	}
}
