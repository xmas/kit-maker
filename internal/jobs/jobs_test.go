package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/docker/kit-maker/internal/jobs"
	"github.com/docker/kit-maker/internal/testutil"
)

func TestWorkerRunsJobs(t *testing.T) {
	db := testutil.StartPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client, err := jobs.NewClient(db.Pool, testutil.Logger(), true)
	if err != nil {
		t.Fatal(err)
	}
	completed, stop := client.Subscribe(river.EventKindJobCompleted)
	defer stop()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Stop(context.Background()) }()

	if _, err := client.Insert(ctx, jobs.EchoArgs{Message: "hello"}, nil); err != nil {
		t.Fatal(err)
	}

	// Expect the echo job and the periodic heartbeat (RunOnStart) to finish.
	seen := map[string]bool{}
	for !seen["echo"] || !seen["heartbeat"] {
		select {
		case ev := <-completed:
			seen[ev.Job.Kind] = true
		case <-ctx.Done():
			t.Fatalf("timed out; completed kinds: %v", seen)
		}
	}
}
