// Command server is the Motif Atlas HTTP API.
//
// Endpoints so far:
//
//	GET /healthz  liveness:  "is this process alive?"  (never touches dependencies)
//	GET /readyz   readiness: "can I serve traffic?"   (pings Postgres and Redis)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// run wires everything together. Keeping it separate from main means
// errors are returned in one place and deferred cleanup always runs.
func run(logger *slog.Logger) error {
	// Stop cleanly on Ctrl+C (SIGINT) or when Docker/Kubernetes sends SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Configuration comes from environment variables (fail fast if missing).
	port := getenv("PORT", "8080")
	databaseURL, err := mustGetenv("DATABASE_URL")
	if err != nil {
		return err
	}
	redisURL, err := mustGetenv("REDIS_URL")
	if err != nil {
		return err
	}

	// 2. Postgres connection pool. Connections are opened lazily, so the
	//    server can start even if Postgres is still booting; /readyz tells
	//    the truth about whether it is reachable.
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	// 3. Redis client.
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	// 4. Routes (Go 1.22+ lets you put the HTTP method in the pattern).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz(logger, db, rdb))

	// 5. An http.Server with timeouts, so slow clients cannot hold connections forever.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 6. Serve in the background; wait for a shutdown signal or a server error.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// 7. Graceful shutdown: stop accepting new requests, let in-flight ones finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// handleHealthz is the liveness probe. It only proves the process can answer
// HTTP. It must not check dependencies: if Postgres is down, restarting this
// process would not fix anything.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is the readiness probe. It returns 200 only if every
// dependency answers, otherwise 503 so a load balancer stops sending traffic.
func handleReadyz(logger *slog.Logger, db *pgxpool.Pool, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		checks := map[string]string{"postgres": "ok", "redis": "ok"}
		status := http.StatusOK

		if err := db.Ping(ctx); err != nil {
			logger.Warn("readiness check failed", "dependency", "postgres", "error", err)
			checks["postgres"] = "unavailable"
			status = http.StatusServiceUnavailable
		}
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Warn("readiness check failed", "dependency", "redis", "error", err)
			checks["redis"] = "unavailable"
			status = http.StatusServiceUnavailable
		}

		overall := "ready"
		if status != http.StatusOK {
			overall = "not ready"
		}
		writeJSON(w, status, map[string]any{"status": overall, "checks": checks})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetenv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", errors.New(key + " is not set")
	}
	return v, nil
}