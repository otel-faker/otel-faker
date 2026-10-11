package cmd

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otel-faker/otel-faker/pkg/emitter"
	"github.com/otel-faker/otel-faker/pkg/generator"
	"github.com/otel-faker/otel-faker/pkg/scenarios"
)

type runOptions struct {
	output   string
	endpoint string
	insecure bool
	rate     float64
	count    uint64
	duration time.Duration
	seed     uint64
	start    string
}

func newRunCmd() *cobra.Command {
	var o runOptions
	cmd := &cobra.Command{
		Use:   "run <scenario>",
		Short: "Run a scenario and emit its telemetry",
		Long: "Run a scenario and emit its telemetry until --count or --duration is reached,\n" +
			"or until interrupted.\n\nScenarios:\n" + scenarioHelp(),
		Example: `  otel-faker run http-spike --count 20
  otel-faker run http-spike --rate 100 --duration 2m --seed 42 --start 2026-01-01T00:00:00Z
  otel-faker run http-spike --output otlp --endpoint localhost:4317 --insecure`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: scenarioNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd, args[0])
		},
	}

	f := cmd.Flags()
	f.StringVarP(&o.output, "output", "o", "stdout", "destination: stdout (OTLP/JSON) or otlp (OTLP/gRPC)")
	f.StringVar(&o.endpoint, "endpoint", "localhost:4317", "OTLP/gRPC endpoint as host:port, for --output otlp")
	f.BoolVar(&o.insecure, "insecure", false, "use plaintext instead of TLS, for --output otlp")
	f.Float64VarP(&o.rate, "rate", "r", 10, "events per second")
	f.Uint64VarP(&o.count, "count", "n", 0, "stop after this many events (0 means no limit)")
	f.DurationVarP(&o.duration, "duration", "d", 0, "stop after this much simulated time (0 means no limit)")
	f.Uint64Var(&o.seed, "seed", 0, "random seed (default: random, printed to stderr)")
	f.StringVar(&o.start, "start", "", "RFC 3339 timestamp of the first event (default: now)")
	return cmd
}

func (o *runOptions) run(cmd *cobra.Command, name string) error {
	info, ok := scenarios.Lookup(name)
	if !ok {
		return fmt.Errorf("unknown scenario %q (available: %s)", name, strings.Join(scenarioNames(), ", "))
	}
	if !(o.rate > 0) || math.IsInf(o.rate, 1) {
		return fmt.Errorf("--rate must be a positive number, got %v", o.rate)
	}

	start := time.Now().UTC()
	if o.start != "" {
		t, err := time.Parse(time.RFC3339Nano, o.start)
		if err != nil {
			return fmt.Errorf("--start: %w", err)
		}
		start = t
	}
	if !cmd.Flags().Changed("seed") {
		o.seed = rand.Uint64()
	}

	em, err := o.newEmitter(cmd.OutOrStdout())
	if err != nil {
		return err
	}

	gen, err := generator.New(info.New(), em, generator.Config{
		Rate:     o.rate,
		Count:    o.count,
		Duration: o.duration,
		Seed:     o.seed,
		Start:    start,
	})
	if err != nil {
		return errors.Join(err, em.Close())
	}

	stderr := cmd.ErrOrStderr()
	fmt.Fprintf(stderr, "otel-faker: scenario=%s output=%s rate=%g seed=%d start=%s\n",
		name, o.output, o.rate, o.seed, start.Format(time.RFC3339Nano))

	stats, runErr := gen.Run(cmd.Context())
	closeErr := em.Close()
	fmt.Fprintf(stderr, "otel-faker: %d events: %d spans, %d log records, %d metric points\n",
		stats.Events, stats.Spans, stats.Logs, stats.MetricPoints)
	return errors.Join(runErr, closeErr)
}

func (o *runOptions) newEmitter(stdout io.Writer) (emitter.Emitter, error) {
	switch o.output {
	case "stdout":
		return emitter.NewJSON(stdout), nil
	case "otlp":
		return emitter.NewOTLP(emitter.OTLPOptions{Endpoint: o.endpoint, Insecure: o.insecure})
	default:
		return nil, fmt.Errorf("unknown --output %q (want stdout or otlp)", o.output)
	}
}

func scenarioNames() []string {
	var names []string
	for _, s := range scenarios.List() {
		names = append(names, s.Name)
	}
	return names
}

func scenarioHelp() string {
	var b strings.Builder
	for _, s := range scenarios.List() {
		fmt.Fprintf(&b, "  %-14s %s\n", s.Name, s.Description)
	}
	return b.String()
}
