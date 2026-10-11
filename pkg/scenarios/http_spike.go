package scenarios

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

func init() {
	Register("http-spike",
		"An HTTP service whose 500s and latency spike for 20s of every minute as its database stalls.",
		func() Scenario { return NewHTTPSpike() })
}

// HTTPSpike simulates "checkout", an HTTP service backed by PostgreSQL.
//
// In every Period of simulated time the service serves Normal traffic, except
// for SpikeDuration starting at SpikeStart, when its database stalls: latency
// climbs and a large share of requests wait out the statement timeout and
// fail with HTTP 500.
//
// Each event is one completed request. It yields a server span, a child
// database client span and an access log record correlated with the trace.
// The cumulative http.server.request.duration histogram is emitted every
// MetricInterval and when the run ends.
type HTTPSpike struct {
	Period        time.Duration
	SpikeStart    time.Duration
	SpikeDuration time.Duration
	Normal        HTTPTraffic
	Spike         HTTPTraffic
	// StatementTimeout is how long a failing request waits on the database.
	StatementTimeout time.Duration
	// MetricInterval is the simulated time between metric snapshots.
	MetricInterval time.Duration

	resource  *telemetry.Resource
	series    map[string]*durationSeries
	start     time.Time
	nextFlush time.Time
	dirty     bool
}

// HTTPTraffic describes requests during one phase of a scenario.
type HTTPTraffic struct {
	// ErrorRate is the probability, from 0 to 1, that a request fails.
	ErrorRate float64
	// MedianLatency is the median duration of a successful request.
	MedianLatency time.Duration
}

// NewHTTPSpike returns an HTTPSpike with its default timings and traffic.
func NewHTTPSpike() *HTTPSpike {
	return &HTTPSpike{
		Period:           time.Minute,
		SpikeStart:       20 * time.Second,
		SpikeDuration:    20 * time.Second,
		Normal:           HTTPTraffic{ErrorRate: 0.005, MedianLatency: 45 * time.Millisecond},
		Spike:            HTTPTraffic{ErrorRate: 0.35, MedianLatency: 850 * time.Millisecond},
		StatementTimeout: 2 * time.Second,
		MetricInterval:   10 * time.Second,
		resource: &telemetry.Resource{Attributes: telemetry.Attributes{
			"service.name":                "checkout",
			"service.version":             "1.4.2",
			"service.instance.id":         "checkout-7d9f8b6c5-x2v8k",
			"deployment.environment.name": "synthetic",
		}},
		series: map[string]*durationSeries{},
	}
}

type httpRoute struct {
	method, route, table, operation string
	status                          int // on success
	weight                          int
}

var httpSpikeRoutes = []httpRoute{
	{"GET", "/api/cart", "carts", "SELECT", 200, 5},
	{"POST", "/api/checkout", "orders", "INSERT", 201, 3},
	{"GET", "/api/orders/{id}", "orders", "SELECT", 200, 2},
}

// Bucket boundaries recommended by the HTTP semantic conventions, in seconds.
var httpDurationBounds = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// durationSeries is the cumulative state of one histogram series.
type durationSeries struct {
	attrs  telemetry.Attributes
	count  uint64
	sum    float64
	counts []uint64
}

// Next simulates one completed request.
func (h *HTTPSpike) Next(t Tick) telemetry.Batch {
	if h.start.IsZero() {
		h.start = t.Time
		h.nextFlush = t.Time.Add(h.MetricInterval)
	}

	traffic := h.Normal
	if h.spiking(t.Elapsed) {
		traffic = h.Spike
	}
	r := pickRoute(t.Rand)
	path := r.route
	if r.route == "/api/orders/{id}" {
		path = fmt.Sprintf("/api/orders/%d", 10000+t.Rand.IntN(90000))
	}
	failed := t.Rand.Float64() < traffic.ErrorRate

	// The request completes at t.Time, so its telemetry is never in the future.
	var total, db time.Duration
	if failed {
		db = h.StatementTimeout
		total = db + time.Duration(3+t.Rand.IntN(12))*time.Millisecond
	} else {
		total = time.Duration(float64(traffic.MedianLatency) * math.Exp(0.4*t.Rand.NormFloat64()))
		total = min(max(total, time.Millisecond), h.StatementTimeout)
		db = time.Duration(float64(total) * (0.6 + 0.3*t.Rand.Float64()))
	}
	end := t.Time
	start := end.Add(-total)
	dbStart := start.Add((total - db) / 3)

	status := r.status
	if failed {
		status = 500
	}

	traceID := telemetry.RandomTraceID(t.Rand)
	server := telemetry.Span{
		Resource: h.resource,
		TraceID:  traceID,
		SpanID:   telemetry.RandomSpanID(t.Rand),
		Name:     r.method + " " + r.route,
		Kind:     telemetry.SpanKindServer,
		Start:    start,
		End:      end,
		Attributes: telemetry.Attributes{
			"http.request.method":       r.method,
			"http.route":                r.route,
			"http.response.status_code": status,
			"url.path":                  path,
			"url.scheme":                "https",
			"server.address":            "checkout.example.com",
			"network.protocol.version":  "1.1",
		},
	}
	query := telemetry.Span{
		Resource:     h.resource,
		TraceID:      traceID,
		SpanID:       telemetry.RandomSpanID(t.Rand),
		ParentSpanID: server.SpanID,
		Name:         r.operation + " " + r.table,
		Kind:         telemetry.SpanKindClient,
		Start:        dbStart,
		End:          dbStart.Add(db),
		Attributes: telemetry.Attributes{
			"db.system.name":     "postgresql",
			"db.namespace":       "checkout",
			"db.operation.name":  r.operation,
			"db.collection.name": r.table,
			"server.address":     "checkout-db",
			"server.port":        5432,
		},
	}
	log := telemetry.LogRecord{
		Resource: h.resource,
		Time:     end,
		Severity: telemetry.SeverityInfo,
		Body:     fmt.Sprintf("%s %s %d %s", r.method, path, status, total.Round(time.Millisecond)),
		Attributes: telemetry.Attributes{
			"http.request.method":       r.method,
			"http.route":                r.route,
			"http.response.status_code": status,
		},
		TraceID: traceID,
		SpanID:  server.SpanID,
	}
	if failed {
		const sqlstate, msg = "57014", "canceling statement due to statement timeout"
		server.Status = telemetry.StatusError
		server.Attributes["error.type"] = "500"
		query.Status, query.StatusMessage = telemetry.StatusError, msg
		query.Attributes["db.response.status_code"] = sqlstate
		query.Attributes["error.type"] = sqlstate
		log.Severity = telemetry.SeverityError
		log.Body += ": " + msg
		log.Attributes["error.type"] = "500"
	}

	b := telemetry.Batch{Spans: []telemetry.Span{server, query}, Logs: []telemetry.LogRecord{log}}
	h.record(r, status, total)
	if h.MetricInterval > 0 && !t.Time.Before(h.nextFlush) {
		b.Metrics = h.snapshot(t.Time)
		for !t.Time.Before(h.nextFlush) {
			h.nextFlush = h.nextFlush.Add(h.MetricInterval)
		}
	}
	return b
}

// Finish emits the metrics recorded since the last snapshot.
func (h *HTTPSpike) Finish(t Tick) telemetry.Batch {
	if !h.dirty {
		return telemetry.Batch{}
	}
	return telemetry.Batch{Metrics: h.snapshot(t.Time)}
}

func (h *HTTPSpike) spiking(elapsed time.Duration) bool {
	if h.Period <= 0 {
		return false
	}
	off := elapsed % h.Period
	return off >= h.SpikeStart && off < h.SpikeStart+h.SpikeDuration
}

func (h *HTTPSpike) record(r httpRoute, status int, d time.Duration) {
	key := r.method + " " + r.route + " " + strconv.Itoa(status)
	s, ok := h.series[key]
	if !ok {
		attrs := telemetry.Attributes{
			"http.request.method":       r.method,
			"http.route":                r.route,
			"http.response.status_code": status,
		}
		if status >= 500 {
			attrs["error.type"] = strconv.Itoa(status)
		}
		s = &durationSeries{attrs: attrs, counts: make([]uint64, len(httpDurationBounds)+1)}
		h.series[key] = s
	}
	v := d.Seconds()
	s.count++
	s.sum += v
	s.counts[sort.SearchFloat64s(httpDurationBounds, v)]++
	h.dirty = true
}

func (h *HTTPSpike) snapshot(at time.Time) []telemetry.Metric {
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	points := make([]telemetry.Point, 0, len(keys))
	for _, k := range keys {
		s := h.series[k]
		points = append(points, telemetry.Point{
			Attributes: s.attrs,
			Start:      h.start,
			Time:       at,
			Histogram: &telemetry.HistogramValue{
				Count:        s.count,
				Sum:          s.sum,
				Bounds:       httpDurationBounds,
				BucketCounts: slices.Clone(s.counts),
			},
		})
	}
	h.dirty = false
	return []telemetry.Metric{{
		Resource:    h.resource,
		Name:        "http.server.request.duration",
		Description: "Duration of HTTP server requests.",
		Unit:        "s",
		Kind:        telemetry.MetricHistogram,
		Points:      points,
	}}
}

func pickRoute(r *rand.Rand) httpRoute {
	total := 0
	for _, rt := range httpSpikeRoutes {
		total += rt.weight
	}
	n := r.IntN(total)
	for _, rt := range httpSpikeRoutes {
		if n < rt.weight {
			return rt
		}
		n -= rt.weight
	}
	panic("unreachable")
}
