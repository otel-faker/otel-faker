// Package scenarios defines the Scenario interface and the registry of
// scenarios available to otel-faker.
//
// To add a scenario, create a file in this package that implements Scenario
// and registers it from an init function:
//
//	func init() {
//		Register("queue-backlog", "A consumer falls behind its queue.",
//			func() Scenario { return &QueueBacklog{} })
//	}
//
// The CLI, generator and emitters need no changes.
package scenarios

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/otel-faker/otel-faker/pkg/telemetry"
)

// Tick describes one event of a run.
type Tick struct {
	// Seq is the event's position in the run, starting at 0.
	Seq uint64
	// Time is the event's simulated timestamp.
	Time time.Time
	// Elapsed is the simulated time since the start of the run.
	Elapsed time.Duration
	// Rand is the run's seeded random source.
	Rand *rand.Rand
}

// Scenario simulates a system and yields the telemetry it would produce.
//
// The generator calls Next once per event, in Seq order, from a single
// goroutine. To keep runs reproducible, an implementation must derive
// everything it produces from the Tick and its own state: use Tick.Time, never
// the wall clock, and Tick.Rand, never another random source.
type Scenario interface {
	Next(t Tick) telemetry.Batch
}

// Finisher is implemented by scenarios that buffer telemetry, such as
// aggregated metrics, and must emit it when a run ends. The generator calls
// Finish once, after the last Next, with the Tick that would have come next.
type Finisher interface {
	Finish(t Tick) telemetry.Batch
}

// Factory returns a new Scenario. Scenarios keep state between events, so
// each run gets its own instance.
type Factory func() Scenario

// Info describes a registered scenario.
type Info struct {
	Name        string
	Description string
	New         Factory
}

var (
	mu       sync.RWMutex
	registry = map[string]Info{}
)

// Register makes a scenario available under name, which should be lower-case
// and hyphenated. It is meant to be called from an init function and panics
// if name is empty, contains whitespace or is already registered.
func Register(name, description string, f Factory) {
	if name == "" || strings.ContainsAny(name, " \t\n") || f == nil {
		panic(fmt.Sprintf("scenarios: invalid registration %q", name))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("scenarios: %q registered twice", name))
	}
	registry[name] = Info{Name: name, Description: description, New: f}
}

// Lookup returns the scenario registered under name.
func Lookup(name string) (Info, bool) {
	mu.RLock()
	defer mu.RUnlock()
	info, ok := registry[name]
	return info, ok
}

// List returns every registered scenario, sorted by name.
func List() []Info {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Info, 0, len(registry))
	for _, info := range registry {
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b Info) int { return strings.Compare(a.Name, b.Name) })
	return out
}
