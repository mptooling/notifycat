// Command notifycat-server starts the HTTP server that receives GitHub
// webhooks and posts to Slack. The dependency graph, the startup-validation
// gate, and the server/scheduler lifecycle live in internal/runtime as an fx
// module; this entrypoint loads config, runs the fx app, and translates a
// fatal startup or shutdown error into a non-zero exit.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/runtime"
)

const shutdownTimeout = 15 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-server:", startupError(err))
		os.Exit(1)
	}

	// fx.New builds the graph, opens+migrates the database, and runs the
	// startup-validation gate synchronously (no timeout, matching the legacy
	// entrypoint). A failing gate, a bad cron spec, or a database error leaves
	// the app in an error state that Start surfaces.
	app := fx.New(
		fx.Supply(cfg),
		runtime.Module,
		fx.NopLogger,
	)

	// Start runs only the OnStart lifecycle hooks (launching the server and
	// scheduler goroutines); the heavy startup work already ran during New.
	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-server:", startupError(err))
		os.Exit(1)
	}

	// Block until a SIGINT/SIGTERM (exit code 0) or a Shutdowner-triggered exit
	// (e.g. a fatal ListenAndServe error → exit code 1).
	sig := <-app.Wait()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer stopCancel()
	if err := app.Stop(stopCtx); err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-server:", fmt.Sprintf("shutdown: %v", err))
		os.Exit(1)
	}
	os.Exit(sig.ExitCode)
}
