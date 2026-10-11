// Package otlpconv converts the telemetry model into OTLP pdata, which both
// emitters serialize: as JSON for stdout and as protobuf for gRPC.
package otlpconv

import (
	"fmt"
	"slices"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

// ScopeName is the instrumentation scope of all generated telemetry.
const ScopeName = "github.com/otel-faker/otel-faker"

// Traces converts spans, grouping them by resource in order of first use.
func Traces(spans []telemetry.Span) ptrace.Traces {
	td := ptrace.NewTraces()
	for res, group := range groupByResource(spans, func(s *telemetry.Span) *telemetry.Resource { return s.Resource }) {
		rs := td.ResourceSpans().AppendEmpty()
		putResource(rs.Resource(), res)
		ss := rs.ScopeSpans().AppendEmpty()
		ss.Scope().SetName(ScopeName)
		out := ss.Spans()
		out.EnsureCapacity(len(group))
		for _, s := range group {
			span := out.AppendEmpty()
			span.SetTraceID(pcommon.TraceID(s.TraceID))
			span.SetSpanID(pcommon.SpanID(s.SpanID))
			span.SetParentSpanID(pcommon.SpanID(s.ParentSpanID))
			span.SetName(s.Name)
			span.SetKind(ptrace.SpanKind(s.Kind))
			span.SetStartTimestamp(pcommon.NewTimestampFromTime(s.Start))
			span.SetEndTimestamp(pcommon.NewTimestampFromTime(s.End))
			putAttributes(span.Attributes(), s.Attributes)
			span.Status().SetCode(ptrace.StatusCode(s.Status))
			span.Status().SetMessage(s.StatusMessage)
		}
	}
	return td
}

// Logs converts log records, grouping them by resource in order of first use.
func Logs(records []telemetry.LogRecord) plog.Logs {
	ld := plog.NewLogs()
	for res, group := range groupByResource(records, func(r *telemetry.LogRecord) *telemetry.Resource { return r.Resource }) {
		rl := ld.ResourceLogs().AppendEmpty()
		putResource(rl.Resource(), res)
		sl := rl.ScopeLogs().AppendEmpty()
		sl.Scope().SetName(ScopeName)
		out := sl.LogRecords()
		out.EnsureCapacity(len(group))
		for _, r := range group {
			lr := out.AppendEmpty()
			ts := pcommon.NewTimestampFromTime(r.Time)
			lr.SetTimestamp(ts)
			lr.SetObservedTimestamp(ts)
			lr.SetSeverityNumber(plog.SeverityNumber(r.Severity))
			lr.SetSeverityText(r.Severity.String())
			lr.Body().SetStr(r.Body)
			putAttributes(lr.Attributes(), r.Attributes)
			lr.SetTraceID(pcommon.TraceID(r.TraceID))
			lr.SetSpanID(pcommon.SpanID(r.SpanID))
		}
	}
	return ld
}

// Metrics converts metrics, grouping them by resource in order of first use.
// Sums and histograms are encoded as cumulative.
func Metrics(metrics []telemetry.Metric) pmetric.Metrics {
	md := pmetric.NewMetrics()
	for res, group := range groupByResource(metrics, func(m *telemetry.Metric) *telemetry.Resource { return m.Resource }) {
		rm := md.ResourceMetrics().AppendEmpty()
		putResource(rm.Resource(), res)
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName(ScopeName)
		out := sm.Metrics()
		out.EnsureCapacity(len(group))
		for _, m := range group {
			metric := out.AppendEmpty()
			metric.SetName(m.Name)
			metric.SetDescription(m.Description)
			metric.SetUnit(m.Unit)
			switch m.Kind {
			case telemetry.MetricGauge:
				dps := metric.SetEmptyGauge().DataPoints()
				for _, p := range m.Points {
					dp := dps.AppendEmpty()
					putAttributes(dp.Attributes(), p.Attributes)
					dp.SetTimestamp(pcommon.NewTimestampFromTime(p.Time))
					dp.SetDoubleValue(p.Value)
				}
			case telemetry.MetricSum:
				sum := metric.SetEmptySum()
				sum.SetIsMonotonic(true)
				sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
				for _, p := range m.Points {
					dp := sum.DataPoints().AppendEmpty()
					putAttributes(dp.Attributes(), p.Attributes)
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(p.Start))
					dp.SetTimestamp(pcommon.NewTimestampFromTime(p.Time))
					dp.SetDoubleValue(p.Value)
				}
			case telemetry.MetricHistogram:
				hist := metric.SetEmptyHistogram()
				hist.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
				for _, p := range m.Points {
					if p.Histogram == nil {
						continue
					}
					dp := hist.DataPoints().AppendEmpty()
					putAttributes(dp.Attributes(), p.Attributes)
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(p.Start))
					dp.SetTimestamp(pcommon.NewTimestampFromTime(p.Time))
					dp.SetCount(p.Histogram.Count)
					dp.SetSum(p.Histogram.Sum)
					dp.ExplicitBounds().FromRaw(p.Histogram.Bounds)
					dp.BucketCounts().FromRaw(p.Histogram.BucketCounts)
				}
			}
		}
	}
	return md
}

// groupByResource yields items grouped by resource pointer. Groups appear in
// order of first use and keep their items' order, so output is deterministic.
func groupByResource[T any](items []T, resource func(*T) *telemetry.Resource) func(yield func(*telemetry.Resource, []*T) bool) {
	return func(yield func(*telemetry.Resource, []*T) bool) {
		var order []*telemetry.Resource
		groups := map[*telemetry.Resource][]*T{}
		for i := range items {
			r := resource(&items[i])
			if _, seen := groups[r]; !seen {
				order = append(order, r)
			}
			groups[r] = append(groups[r], &items[i])
		}
		for _, r := range order {
			if !yield(r, groups[r]) {
				return
			}
		}
	}
}

func putResource(dst pcommon.Resource, r *telemetry.Resource) {
	if r != nil {
		putAttributes(dst.Attributes(), r.Attributes)
	}
}

// putAttributes copies attrs into dst in sorted key order.
func putAttributes(dst pcommon.Map, attrs telemetry.Attributes) {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	dst.EnsureCapacity(len(keys))
	for _, k := range keys {
		switch v := attrs[k].(type) {
		case string:
			dst.PutStr(k, v)
		case bool:
			dst.PutBool(k, v)
		case int:
			dst.PutInt(k, int64(v))
		case int64:
			dst.PutInt(k, v)
		case float64:
			dst.PutDouble(k, v)
		default:
			dst.PutStr(k, fmt.Sprint(v))
		}
	}
}
