// Package generator drives a scenario at a fixed event rate and hands the
// telemetry it yields to an emitter.
//
// A run has two clocks. The simulated clock dates every event: event n happens
// at Config.Start + n/Config.Rate seconds, whatever the wall clock says. The
// wall clock only paces delivery: a batch is emitted once the wall clock has
// advanced as far past the run's start as the batch's last event. Because the
// telemetry depends only on the simulated clock and the seed, the same Config
// always produces the same output.
package generator

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"github.com/otel-faker/otel-faker/pkg/emitter"
	"github.com/otel-faker/otel-faker/pkg/scenarios"
	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

// maxEmitsPerSecond caps how often the emitter is called. Above this rate,
// consecutive events are grouped into one batch.
const maxEmitsPerSecond = 50

// finishTimeout bounds the final flush after a run is interrupted.
const finishTimeout = 5 * time.Second

// Config controls a run.
type Config struct {
	// Rate is the number of events per second of simulated time. Must be > 0.
	Rate float64
	// Count stops the run after this many events. Zero means no limit.
	Count uint64
	// Duration stops the run once this much simulated time has elapsed.
	// Zero means no limit. With both limits set, the first one reached wins.
	Duration time.Duration
	// Seed seeds the random source passed to the scenario.
	Seed uint64
	// Start is the simulated time of the first event. Zero means time.Now().
	Start time.Time
}

// Stats counts what a run emitted.
type Stats struct {
	Events       uint64
	Spans        uint64
	Logs         uint64
	MetricPoints uint64
}

// Generator runs one scenario into one emitter. It is not safe for
// concurrent use, and Run should be called only once.
type Generator struct {
	scenario  scenarios.Scenario
	emitter   emitter.Emitter
	cfg       Config
	rng       *rand.Rand
	limit     uint64 // 0 means unlimited
	batchSize uint64
}

// New returns a Generator for the given scenario, emitter and config.
func New(s scenarios.Scenario, e emitter.Emitter, cfg Config) (*Generator, error) {
	if s == nil || e == nil {
		return nil, errors.New("generator: scenario and emitter are required")
	}
	if !(cfg.Rate > 0) || math.IsInf(cfg.Rate, 1) {
		return nil, errors.New("generator: rate must be a positive, finite number")
	}
	if cfg.Duration < 0 {
		return nil, errors.New("generator: duration must not be negative")
	}
	if cfg.Start.IsZero() {
		cfg.Start = time.Now()
	}

	limit := cfg.Count
	if cfg.Duration > 0 {
		n := uint64(math.Ceil(cfg.Duration.Seconds() * cfg.Rate))
		if limit == 0 || n < limit {
			limit = n
		}
	}

	return &Generator{
		scenario:  s,
		emitter:   e,
		cfg:       cfg,
		rng:       rand.New(rand.NewPCG(cfg.Seed, 0)),
		limit:     limit,
		batchSize: uint64(max(1, math.Ceil(cfg.Rate/maxEmitsPerSecond))),
	}, nil
}

// Run generates events until the configured limit is reached or ctx is
// canceled, then lets the scenario flush buffered telemetry. Cancellation is a
// normal way to stop and is not reported as an error; an emitter failure is.
//
// If the emitter falls behind, Run emits back to back until it catches up.
// It never drops events.
func (g *Generator) Run(ctx context.Context) (Stats, error) {
	var st Stats
	begin := time.Now()
	var seq uint64
	for g.limit == 0 || seq < g.limit {
		n := g.batchSize
		if g.limit > 0 {
			n = min(n, g.limit-seq)
		}
		if !sleepUntil(ctx, begin.Add(g.offset(seq+n-1))) {
			break
		}

		var b telemetry.Batch
		for range n {
			b.Append(g.scenario.Next(g.tick(seq)))
			seq++
		}
		if err := g.emit(ctx, b, &st); err != nil {
			if ctx.Err() != nil {
				break // interrupted mid-export
			}
			return st, err
		}
		st.Events += n
	}
	return st, g.finish(ctx, seq, &st)
}

// finish gives a Finisher scenario the chance to flush, even after ctx is
// canceled.
func (g *Generator) finish(ctx context.Context, seq uint64, st *Stats) error {
	f, ok := g.scenario.(scenarios.Finisher)
	if !ok {
		return nil
	}
	b := f.Finish(g.tick(seq))
	if b.Empty() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	return g.emit(ctx, b, st)
}

func (g *Generator) emit(ctx context.Context, b telemetry.Batch, st *Stats) error {
	if b.Empty() {
		return nil
	}
	if err := g.emitter.Emit(ctx, b); err != nil {
		return err
	}
	st.Spans += uint64(len(b.Spans))
	st.Logs += uint64(len(b.Logs))
	st.MetricPoints += uint64(b.MetricPoints())
	return nil
}

func (g *Generator) tick(seq uint64) scenarios.Tick {
	off := g.offset(seq)
	return scenarios.Tick{Seq: seq, Time: g.cfg.Start.Add(off), Elapsed: off, Rand: g.rng}
}

// offset is the simulated time of event seq relative to the start of the run.
// It is computed from seq rather than accumulated, so rounding never drifts.
func (g *Generator) offset(seq uint64) time.Duration {
	return time.Duration(float64(seq) / g.cfg.Rate * float64(time.Second))
}

// sleepUntil waits until t and reports whether ctx is still live.
func sleepUntil(ctx context.Context, t time.Time) bool {
	d := time.Until(t)
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
