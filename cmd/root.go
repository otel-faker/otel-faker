// Package cmd implements the otel-faker command line.
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// version is the release version. Release builds override it with
// -ldflags "-X github.com/otel-faker/otel-faker/cmd.version=<version>".
var version = "0.1.0"

// Execute runs the command line. An interrupt or SIGTERM cancels the run
// gracefully: buffered telemetry is flushed before the process exits.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newRootCmd().ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "otel-faker",
		Short: "Generate synthetic OpenTelemetry logs, metrics and traces",
		Long: `otel-faker runs a scenario, a small model of a system and its failure modes,
at a fixed event rate and emits the telemetry it yields as pretty-printed
OTLP/JSON on stdout or over OTLP/gRPC.

Runs are reproducible: the same scenario, --seed, --start and --rate produce
the same telemetry.`,
		Version: version,
		// Usage is for argument mistakes, not for failures during a run.
		SilenceUsage: true,
	}
	root.AddCommand(newRunCmd())
	return root
}
