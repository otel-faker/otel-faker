# otel-faker

Synthetic OpenTelemetry logs, metrics and traces for testing observability
pipelines, dashboards and alerts, without running a real application.

otel-faker runs a **scenario**, a small model of a service and the way it
fails, at a fixed rate and emits the telemetry it produces: pretty-printed
OTLP/JSON on stdout, or OTLP/gRPC to an OpenTelemetry Collector or any other
OTLP receiver.

Runs are reproducible. Each event is dated on a simulated clock (`--start` plus
`n / --rate` seconds) and every random choice comes from `--seed`, so the same
flags produce the same telemetry, byte for byte.

## Install

Requires Go 1.26 or newer.

```sh
go install github.com/otel-faker/otel-faker@latest
```

## Run the http-spike scenario

`http-spike` simulates `checkout`, an HTTP service backed by PostgreSQL. For 20
seconds of every minute (0:20 to 0:40) its database stalls: median latency
climbs from about 45 ms to over a second, and about 35% of requests fail with
HTTP 500 after a 2 s statement timeout.

Each request yields a server span, a child database span and an access log
record correlated with the trace. The `http.server.request.duration` histogram
is emitted every 10 s of simulated time and when the run ends.

Print 20 requests to stdout:

```sh
otel-faker run http-spike --count 20
```

Watch a full spike and print only the failing requests:

```sh
otel-faker run http-spike --duration 1m \
  | jq -r '.resourceLogs[]?.scopeLogs[].logRecords[] | select(.severityText == "ERROR") | .body.stringValue'
```

Replay a run exactly. Every run prints its seed and start time on stderr:

```sh
otel-faker run http-spike --count 500 --rate 50 --seed 42 --start 2026-01-01T00:00:00Z > run.json
```

Without `--count` or `--duration` the run continues until you press Ctrl-C;
buffered metrics are flushed before it exits.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-o, --output` | `stdout` | `stdout` (OTLP/JSON) or `otlp` (OTLP/gRPC) |
| `--endpoint` | `localhost:4317` | OTLP/gRPC receiver as `host:port` |
| `--insecure` | off | Plaintext instead of TLS |
| `-r, --rate` | `10` | Events per second |
| `-n, --count` | no limit | Stop after this many events |
| `-d, --duration` | no limit | Stop after this much simulated time |
| `--seed` | random | Random seed |
| `--start` | now | RFC 3339 timestamp of the first event |

To send the same telemetry to a local Collector:

```sh
otel-faker run http-spike --output otlp --endpoint localhost:4317 --insecure
```

## Add a scenario

Scenarios live in `pkg/scenarios`. Implement the one-method `Scenario`
interface in a new file and register it from `init`; the CLI, generator and
emitters pick it up unchanged. Use `Tick.Time` and `Tick.Rand`, never the wall
clock or another random source, so runs stay reproducible. `http_spike.go` is a
complete example.

| Path | Contents |
| --- | --- |
| `cmd/` | Command line |
| `pkg/scenarios/` | `Scenario` interface, registry and scenarios |
| `pkg/generator/` | Rate-paced generation loop |
| `pkg/emitter/` | Outputs: stdout JSON and OTLP/gRPC |
| `pkg/telemetry/` | Data model shared by scenarios and emitters |
| `internal/otlpconv/` | Conversion to OTLP |

## License

[MIT](LICENSE)
