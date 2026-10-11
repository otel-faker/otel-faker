package emitter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/otel-faker/otel-faker/internal/otlpconv"
	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

// JSON writes each signal in a batch as a pretty-printed OTLP/JSON document:
// one {"resourceSpans": ...}, {"resourceLogs": ...} or {"resourceMetrics": ...}
// object per signal present, each followed by a newline. The documents are
// the same payloads the OTLP emitter sends, so the output previews the wire.
type JSON struct {
	w      *bufio.Writer
	indent bytes.Buffer
}

// NewJSON returns a JSON emitter that writes to w, typically os.Stdout.
func NewJSON(w io.Writer) *JSON {
	return &JSON{w: bufio.NewWriter(w)}
}

// Emit writes b and flushes, so output streams to pipes as it is generated.
func (j *JSON) Emit(_ context.Context, b telemetry.Batch) error {
	if len(b.Spans) > 0 {
		if err := j.write((&ptrace.JSONMarshaler{}).MarshalTraces(otlpconv.Traces(b.Spans))); err != nil {
			return err
		}
	}
	if len(b.Logs) > 0 {
		if err := j.write((&plog.JSONMarshaler{}).MarshalLogs(otlpconv.Logs(b.Logs))); err != nil {
			return err
		}
	}
	if len(b.Metrics) > 0 {
		if err := j.write((&pmetric.JSONMarshaler{}).MarshalMetrics(otlpconv.Metrics(b.Metrics))); err != nil {
			return err
		}
	}
	return j.w.Flush()
}

func (j *JSON) write(doc []byte, err error) error {
	if err != nil {
		return err
	}
	j.indent.Reset()
	if err := json.Indent(&j.indent, doc, "", "  "); err != nil {
		return err
	}
	j.indent.WriteByte('\n')
	_, err = j.w.Write(j.indent.Bytes())
	return err
}

// Close flushes any buffered output. It does not close the underlying writer.
func (j *JSON) Close() error {
	return j.w.Flush()
}
