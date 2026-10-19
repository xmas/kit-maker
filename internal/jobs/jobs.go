// Package jobs holds River workers and periodic jobs. River stores its queue
// in Postgres (schema "river"), so jobs can be enqueued in the same
// transaction as the rows they act on.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Schema is the Postgres schema holding River's tables, kept apart from the
// Atlas-managed app schema in public.
const Schema = "river"

// EchoArgs is the example job: it logs a message. Real jobs (compile,
// publish, AI, rebases) follow the same shape.
type EchoArgs struct {
	Message string `json:"message"`
}

func (EchoArgs) Kind() string { return "echo" }

type EchoWorker struct {
	river.WorkerDefaults[EchoArgs]
	Log *slog.Logger
}

func (w *EchoWorker) Work(ctx context.Context, job *river.Job[EchoArgs]) error {
	w.Log.InfoContext(ctx, "echo", "message", job.Args.Message, "job_id", job.ID, "attempt", job.Attempt)
	return nil
}

// HeartbeatArgs is the example periodic job. It stands in for the scheduled
// work in DESIGN.md §11.1 (stale sandboxes, upstream schema diff, reference
// scans, directory sync).
type HeartbeatArgs struct{}

func (HeartbeatArgs) Kind() string { return "heartbeat" }

type HeartbeatWorker struct {
	river.WorkerDefaults[HeartbeatArgs]
	Log *slog.Logger
}

func (w *HeartbeatWorker) Work(ctx context.Context, job *river.Job[HeartbeatArgs]) error {
	w.Log.InfoContext(ctx, "heartbeat", "scheduled_at", job.ScheduledAt)
	return nil
}

// HeartbeatInterval is how often the periodic heartbeat runs.
const HeartbeatInterval = time.Minute

// Workers registers every worker.
func Workers(log *slog.Logger) *river.Workers {
	w := river.NewWorkers()
	river.AddWorker(w, &EchoWorker{Log: log})
	river.AddWorker(w, &HeartbeatWorker{Log: log})
	return w
}

// PeriodicJobs lists the scheduled jobs. River elects one leader among
// workers, so each runs once per interval however many replicas there are.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(HeartbeatInterval),
			func() (river.JobArgs, *river.InsertOpts) { return HeartbeatArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}
}

// NewClient returns a River client. With work=false it can only insert jobs
// (for `serve`); with work=true it also runs workers and periodic jobs.
func NewClient(pool *pgxpool.Pool, log *slog.Logger, work bool) (*river.Client[pgx.Tx], error) {
	cfg := &river.Config{
		Schema: Schema,
		Logger: log,
	}
	if work {
		cfg.Workers = Workers(log)
		cfg.PeriodicJobs = PeriodicJobs()
		cfg.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		}
	}
	return river.NewClient(riverpgxv5.New(pool), cfg)
}
