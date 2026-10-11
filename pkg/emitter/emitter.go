// Package emitter delivers telemetry batches to a destination.
//
// To add an output, implement Emitter and select it in cmd/run.go.
package emitter

import (
	"context"

	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

// Emitter delivers telemetry. The generator calls Emit from a single
// goroutine, then Close once when the run ends.
type Emitter interface {
	// Emit delivers one batch, returning once it has been written or sent.
	Emit(ctx context.Context, b telemetry.Batch) error
	// Close flushes buffered data and releases resources.
	Close() error
}
