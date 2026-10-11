// Package telemetry is the small data model that scenarios produce and
// emitters consume. It mirrors the shape of OTLP without depending on any
// OpenTelemetry module, so scenarios stay short and easy to test.
package telemetry

import (
	"encoding/binary"
	"math/rand/v2"
	"time"
)

// Attributes are key/value pairs attached to resources, spans, log records and
// metric points. Values should be string, bool, int, int64 or float64; any
// other type is encoded as its fmt.Sprint string. Emitters write keys in
// sorted order, so map iteration order never changes the output.
type Attributes map[string]any

// Resource describes the entity, usually a service, that produced telemetry.
//
// Emitters group records by Resource pointer: allocate one Resource per
// simulated service and reuse it for every record that service produces.
type Resource struct {
	Attributes Attributes
}

// TraceID is a W3C trace ID. The zero value is invalid.
type TraceID [16]byte

// SpanID is a W3C span ID. The zero value means "no span".
type SpanID [8]byte

// RandomTraceID returns a non-zero trace ID drawn from r.
func RandomTraceID(r *rand.Rand) TraceID {
	var id TraceID
	for id == (TraceID{}) {
		binary.BigEndian.PutUint64(id[:8], r.Uint64())
		binary.BigEndian.PutUint64(id[8:], r.Uint64())
	}
	return id
}

// RandomSpanID returns a non-zero span ID drawn from r.
func RandomSpanID(r *rand.Rand) SpanID {
	var id SpanID
	for id == (SpanID{}) {
		binary.BigEndian.PutUint64(id[:], r.Uint64())
	}
	return id
}

// SpanKind values match OTLP.
type SpanKind int

const (
	SpanKindUnspecified SpanKind = iota
	SpanKindInternal
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// StatusCode values match OTLP.
type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

// Span is one operation within a trace.
type Span struct {
	Resource      *Resource
	TraceID       TraceID
	SpanID        SpanID
	ParentSpanID  SpanID // zero for a root span
	Name          string
	Kind          SpanKind
	Start, End    time.Time
	Attributes    Attributes
	Status        StatusCode
	StatusMessage string
}

// Severity is a log severity number from the OpenTelemetry log data model.
type Severity int

const (
	SeverityTrace Severity = 1
	SeverityDebug Severity = 5
	SeverityInfo  Severity = 9
	SeverityWarn  Severity = 13
	SeverityError Severity = 17
	SeverityFatal Severity = 21
)

// String returns the conventional severity text, such as "ERROR".
func (s Severity) String() string {
	switch {
	case s >= SeverityFatal:
		return "FATAL"
	case s >= SeverityError:
		return "ERROR"
	case s >= SeverityWarn:
		return "WARN"
	case s >= SeverityInfo:
		return "INFO"
	case s >= SeverityDebug:
		return "DEBUG"
	case s >= SeverityTrace:
		return "TRACE"
	default:
		return ""
	}
}

// LogRecord is one log event. Set TraceID and SpanID to correlate it with a
// span; leave them zero otherwise.
type LogRecord struct {
	Resource   *Resource
	Time       time.Time
	Severity   Severity
	Body       string
	Attributes Attributes
	TraceID    TraceID
	SpanID     SpanID
}

// MetricKind selects how a Metric's points are interpreted.
type MetricKind int

const (
	// MetricGauge points carry an instantaneous Value.
	MetricGauge MetricKind = iota + 1
	// MetricSum points carry a monotonic, cumulative Value.
	MetricSum
	// MetricHistogram points carry a cumulative, explicit-bucket Histogram.
	MetricHistogram
)

// Metric is a named stream of points of one kind.
type Metric struct {
	Resource    *Resource
	Name        string
	Description string
	Unit        string
	Kind        MetricKind
	Points      []Point
}

// Point is one data point of a Metric.
type Point struct {
	Attributes Attributes
	Start      time.Time // start of the cumulative window; ignored for gauges
	Time       time.Time
	Value      float64         // gauges and sums
	Histogram  *HistogramValue // histograms
}

// HistogramValue is an explicit-bucket histogram. BucketCounts has
// len(Bounds)+1 entries; bucket i counts values in (Bounds[i-1], Bounds[i]].
type HistogramValue struct {
	Count        uint64
	Sum          float64
	Bounds       []float64
	BucketCounts []uint64
}

// Batch is the telemetry produced by one or more events.
type Batch struct {
	Spans   []Span
	Logs    []LogRecord
	Metrics []Metric
}

// Append adds the records of o to b.
func (b *Batch) Append(o Batch) {
	b.Spans = append(b.Spans, o.Spans...)
	b.Logs = append(b.Logs, o.Logs...)
	b.Metrics = append(b.Metrics, o.Metrics...)
}

// Empty reports whether b holds no records.
func (b Batch) Empty() bool {
	return len(b.Spans) == 0 && len(b.Logs) == 0 && len(b.Metrics) == 0
}

// MetricPoints returns the total number of points across b's metrics.
func (b Batch) MetricPoints() int {
	n := 0
	for _, m := range b.Metrics {
		n += len(m.Points)
	}
	return n
}
