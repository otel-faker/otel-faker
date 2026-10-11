package emitter

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/otel-faker/otel-faker/internal/otlpconv"
	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

// OTLPOptions configures an OTLP emitter.
type OTLPOptions struct {
	// Endpoint is the receiver's host:port, such as "localhost:4317".
	Endpoint string
	// Insecure sends plaintext instead of TLS.
	Insecure bool
	// Timeout bounds each export call. Zero means 10 seconds.
	Timeout time.Duration
}

// OTLP exports batches over OTLP/gRPC, for example to an OpenTelemetry
// Collector. Each Emit makes one export call per signal present in the batch.
type OTLP struct {
	conn    *grpc.ClientConn
	traces  ptraceotlp.GRPCClient
	logs    plogotlp.GRPCClient
	metrics pmetricotlp.GRPCClient
	timeout time.Duration
}

// NewOTLP returns an OTLP emitter. The connection is established lazily, so
// an unreachable endpoint is reported by the first Emit.
func NewOTLP(opts OTLPOptions) (*OTLP, error) {
	if opts.Endpoint == "" {
		return nil, errors.New("otlp: endpoint is required")
	}
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if opts.Insecure {
		creds = insecure.NewCredentials()
	}
	conn, err := grpc.NewClient(opts.Endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("otlp: %w", err)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &OTLP{
		conn:    conn,
		traces:  ptraceotlp.NewGRPCClient(conn),
		logs:    plogotlp.NewGRPCClient(conn),
		metrics: pmetricotlp.NewGRPCClient(conn),
		timeout: timeout,
	}, nil
}

// Emit exports b. A receiver that rejects any record is reported as an error.
func (o *OTLP) Emit(ctx context.Context, b telemetry.Batch) error {
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	if len(b.Spans) > 0 {
		resp, err := o.traces.Export(ctx, ptraceotlp.NewExportRequestFromTraces(otlpconv.Traces(b.Spans)))
		if err != nil {
			return fmt.Errorf("otlp: export traces: %w", err)
		}
		if p := resp.PartialSuccess(); p.RejectedSpans() > 0 {
			return fmt.Errorf("otlp: receiver rejected %d spans: %s", p.RejectedSpans(), p.ErrorMessage())
		}
	}
	if len(b.Logs) > 0 {
		resp, err := o.logs.Export(ctx, plogotlp.NewExportRequestFromLogs(otlpconv.Logs(b.Logs)))
		if err != nil {
			return fmt.Errorf("otlp: export logs: %w", err)
		}
		if p := resp.PartialSuccess(); p.RejectedLogRecords() > 0 {
			return fmt.Errorf("otlp: receiver rejected %d log records: %s", p.RejectedLogRecords(), p.ErrorMessage())
		}
	}
	if len(b.Metrics) > 0 {
		resp, err := o.metrics.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(otlpconv.Metrics(b.Metrics)))
		if err != nil {
			return fmt.Errorf("otlp: export metrics: %w", err)
		}
		if p := resp.PartialSuccess(); p.RejectedDataPoints() > 0 {
			return fmt.Errorf("otlp: receiver rejected %d data points: %s", p.RejectedDataPoints(), p.ErrorMessage())
		}
	}
	return nil
}

// Close closes the gRPC connection.
func (o *OTLP) Close() error {
	return o.conn.Close()
}
