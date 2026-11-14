package db

import "mikrodash/internal/activity"

// One workspace per DB connection, protected by activityMu. Between requests the
// connection returns to the pool so ingestion can proceed. The TEMP workspace
// contains a complete frozen source snapshot; no partial result enters the cache.
type activityAggregateJob struct {
	key     string
	stage   int
	large   bool
	expires int64
	result  activity.Result
}

type activityAggregateRequest struct{ Key string }

func activityAggregatePending(f activity.Filter) activity.Result {
	out := EmptyActivityResult(f)
	out.Preparing = true
	return out
}
