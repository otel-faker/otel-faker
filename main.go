// Command otel-faker generates synthetic OpenTelemetry logs, metrics and
// traces that simulate application traffic and failure modes.
package main

import (
	"os"

	"github.com/otel-faker/otel-faker/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
