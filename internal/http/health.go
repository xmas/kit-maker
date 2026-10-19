package http

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"time"

	"github.com/docker/kit-maker/internal/dbmigrate"
)

// healthz is liveness: the process is up and serving.
func (s *server) healthz(w nethttp.ResponseWriter, _ *nethttp.Request) {
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

// readyz is readiness: both required services answer and the schema matches
// this binary.
func (s *server) readyz(w nethttp.ResponseWriter, r *nethttp.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	ok := true
	record := func(name string, err error) {
		if err != nil {
			checks[name] = err.Error()
			ok = false
			return
		}
		checks[name] = "ok"
	}

	dbErr := s.DB.PingContext(ctx)
	record("database", dbErr)
	if dbErr == nil {
		record("migrations", dbmigrate.Check(ctx, s.DB))
	} else {
		record("migrations", errors.New("skipped: database unavailable"))
	}
	accessible, err := s.Bucket.IsAccessible(ctx)
	if err == nil && !accessible {
		err = errors.New("bucket not accessible")
	}
	record("storage", err)

	status, code := "ok", nethttp.StatusOK
	if !ok {
		status, code = "unavailable", nethttp.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{"status": status, "checks": checks})
}

func writeJSON(w nethttp.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
