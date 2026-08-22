# EdgeLens

EdgeLens is a Linux telemetry and performance-investigation tool. It has two related operating modes:

1. **Continuous telemetry** samples host CPU, memory, disk, network, and temperature data.
2. **Experiment mode** launches one explicit workload, captures Linux process metrics, records `perf stat` counters, optionally samples call stacks for a flame chart, and optionally analyzes a Go pprof heap profile.

The agent sends bounded JSON packets over UDP. The collector validates and stores them in SQLite. The dashboard reads that database through a local HTTP API and presents host trends and experiment evidence.

EdgeLens is intended for a trusted development or performance lab. It is useful for learning and investigating Linux behavior relevant to scientific computing, HPC, low-latency systems, and CPU-intensive services. It does not automatically prove the cause of a performance problem; it preserves evidence that an engineer can interpret.

## Current capabilities

- Continuous Linux host telemetry from `/proc`, `/sys`, and `statfs`.
- Explicit child-process execution with an argument vector, never an implicit shell.
- Fail-closed `perf` preflight before a workload is launched.
- Hardware and software counters through `perf stat`.
- Process CPU, RSS, virtual memory, data/heap mapping, I/O, thread, state, and page-fault sampling.
- Optional sampled call stacks through `perf record` and an interactive flame chart.
- Optional Go pprof heap-profile summary through `go tool pprof`.
- Context and signal cancellation of the complete perf/workload process group.
- Separately versioned experiment events while preserving the original v1 host-measurement format.
- Bounded UTF-8 evidence artifacts with byte counts and SHA-256 checksums.
- Idempotent SQLite writes for duplicate UDP packets.
- Read-only HTTP APIs and an embedded browser dashboard.

## Current scope and non-goals

The agent is Linux-only because it reads procfs and uses Linux `perf`. The collector and dashboard are ordinary Go programs and can run anywhere supported by the SQLite driver, but the documented deployment is Linux.

The current release does **not** provide:

- Dashboard controls that launch arbitrary programs.
- Shell expression evaluation, pipelines, redirects, or glob expansion.
- Guaranteed UDP delivery, ordering, acknowledgement, or retransmission.
- Automatic causal conclusions or anomaly explanations.
- `perf record` binary retention or arbitrary unbounded traces.
- JVM HPROF, jemalloc, core-dump, or native allocator heap parsers.
- GPU vendor telemetry such as CUDA, ROCm, or Metal counters.
- eBPF/BCC tracing, NUMA/topology discovery, or cross-host clock synchronization.
- Authentication, authorization, TLS, automatic retention, or multi-tenant isolation.

## Requirements

### Build requirements

- Go version declared by [`go.mod`](go.mod), currently Go `1.26.5`.
- A C compiler is not required; the project uses the pure-Go `modernc.org/sqlite` driver.

### Agent host requirements

- Linux with readable `/proc` and `/sys` metric files.
- `perf` installed for the running kernel.
- Permission to use the requested counters and, when enabled, sampled stack recording.
- A valid block-device name and network-interface name.
- `go` available at runtime only when `-heap-profile` is used, because the agent invokes `go tool pprof` after the workload exits.

Check the host before running an experiment:

```bash
go version
perf --version
cat /proc/sys/kernel/perf_event_paranoid
perf stat -e task-clock,cycles,instructions -- true
```

For flame collection, test stack sampling separately:

```bash
profile="$(mktemp)"
perf record -q -o "$profile" -g --call-graph dwarf -- true
perf script -i "$profile" >/dev/null
rm -f "$profile"
```

Counter access and stack-sampling access are distinct kernel decisions. Passing `perf stat` does not prove that `perf record` will work.

## Quick start

Run these commands from the repository root in separate terminals.

### 1. Identify local metric sources

```bash
lsblk
ip link
```

Common disk names include `mmcblk0`, `nvme0n1`, `sda`, and `vda`. Common interface names include `eth0`, `wlan0`, `enp0s3`, and `lo`.

### 2. Start the collector

```bash
go run ./cmd/collector \
  -udp-address 127.0.0.1:9000 \
  -database measurements.db
```

The collector owns writes to the database.

### 3. Start the dashboard

```bash
go run ./cmd/dashboard \
  -address 127.0.0.1:8080 \
  -database measurements.db
```

The collector and dashboard must use the **same database path**. Open:

http://127.0.0.1:8080

### 4. Start continuous host telemetry

Replace the device and interface with values from your machine:

```bash
go run ./cmd/agent \
  -collector 127.0.0.1:9000 \
  -host workstation-01 \
  -disk-device nvme0n1 \
  -disk-mount / \
  -network-interface enp0s3 \
  -report-interval 5s
```

If `-run-command` is absent, the agent stays in continuous mode until interrupted.

## Running performance experiments

Experiment mode is enabled by `-run-command`. The command and every argument are separate values. EdgeLens passes them to `exec.CommandContext`; it does not concatenate them into a shell command.

### Counter-only example

```bash
go run ./cmd/agent \
  -collector 127.0.0.1:9000 \
  -host workstation-01 \
  -disk-device nvme0n1 \
  -disk-mount / \
  -network-interface enp0s3 \
  -report-interval 1s \
  -run-id sha256-perf-binary \
  -run-command /usr/bin/sha256sum \
  -run-arg /usr/bin/perf
```

This records host telemetry concurrently, process observations when the process lives long enough to sample, and the default `perf stat` counters.

### Flame-chart example

This is representative of the OpenSSL workload used for end-to-end verification:

```bash
go run ./cmd/agent \
  -collector 127.0.0.1:9000 \
  -host workstation-01 \
  -disk-device nvme0n1 \
  -disk-mount / \
  -network-interface enp0s3 \
  -report-interval 1s \
  -run-id openssl-sha256 \
  -run-command /usr/bin/openssl \
  -run-arg speed \
  -run-arg=-seconds \
  -run-arg 1 \
  -run-arg sha256 \
  -perf-events task-clock,cycles,instructions,branches,branch-misses,cache-references,cache-misses,context-switches,page-faults \
  -artifact-max-bytes 49152 \
  -flamegraph
```

Use `-run-arg=value` when an argument begins with `-`. This prevents Go's flag parser from interpreting the workload argument as an agent flag.

Build native workloads with symbols when possible. For C and C++, `-g` and `-fno-omit-frame-pointer` usually improve stack readability. Optimization, inlining, stripped binaries, JIT compilation, and unavailable kernel symbols can still produce incomplete or unfamiliar stacks.

### Go heap-profile example

EdgeLens does not force another Go process to dump its heap. The workload must write a pprof profile itself. A Go test binary supports `-test.memprofile`:

```bash
go test -c -o /tmp/experiment.test ./internal/experiment

go run ./cmd/agent \
  -collector 127.0.0.1:9000 \
  -host workstation-01 \
  -disk-device nvme0n1 \
  -disk-mount / \
  -network-interface enp0s3 \
  -report-interval 1s \
  -run-id experiment-heap \
  -run-command /tmp/experiment.test \
  -run-arg=-test.run=TestNewRunningRun \
  -run-arg=-test.memprofile=/tmp/experiment-heap.pb.gz \
  -heap-profile /tmp/experiment-heap.pb.gz \
  -perf-events task-clock,cycles,instructions
```

After the child exits, the agent runs the bounded equivalent of:

```bash
go tool pprof \
  -top \
  -nodecount=80 \
  -sample_index=inuse_space \
  /tmp/experiment-heap.pb.gz
```

The resulting text is stored as the `heap-summary` artifact.

### Cancellation

Press `Ctrl+C` in the agent terminal to cancel an experiment. The signal-aware context requests termination of the complete perf/workload process group. The wait goroutine still reaps the child, and the agent attempts to publish one `interrupted` terminal event.

UDP is best effort, so the terminal packet can still be lost if connectivity is unavailable during cancellation.

## Agent flags

Run `go run ./cmd/agent -h` for the executable's authoritative flag list.

| Flag | Default | Meaning |
|---|---:|---|
| `-collector` | `127.0.0.1:9000` | Collector UDP destination as `host:port`. |
| `-host` | OS hostname | Logical host identity stored with measurements and runs. |
| `-report-interval` | `5s` | Host rate-observation window and process-sampling interval. Minimum `1s`. |
| `-disk-device` | `mmcblk0` | Linux block device used for disk I/O rates. |
| `-disk-mount` | `/` | Mounted filesystem used for disk-space usage. |
| `-network-interface` | `eth0` | Interface used for receive/send rates. |
| `-run-command` | empty | Explicit workload executable. Its presence enables experiment mode. |
| `-run-arg` | empty | One exact workload argument. Repeat for each argument. |
| `-run-id` | generated | Stable run ID. Generated as random 128-bit hexadecimal text when omitted. |
| `-perf-events` | conservative set | Comma-separated event names passed to perf. |
| `-artifact-max-bytes` | `32768` | Maximum text bytes per artifact. Must be `1..49152`. |
| `-flamegraph` | `false` | Enable `perf record`, folded-stack generation, and dashboard flame rendering. |
| `-heap-profile` | empty | Go pprof heap-profile path to analyze after child exit. |

The default perf event set is:

```text
task-clock
cycles
instructions
branches
branch-misses
cache-references
cache-misses
context-switches
cpu-migrations
page-faults
```

Not every CPU, kernel, virtual machine, container, or Raspberry Pi supports every event. Override `-perf-events` with a set supported by the target.

## Collector flags

```text
-database     SQLite path, default measurements.db
-udp-address  UDP bind address, default :9000
```

Examples:

```bash
# Local-only collector
go run ./cmd/collector -udp-address 127.0.0.1:9000

# Listen on available interfaces for trusted LAN agents
go run ./cmd/collector \
  -udp-address :9000 \
  -database "$HOME/.local/share/edgelens/measurements.db"
```

`127.0.0.1:9000` accepts only local traffic. `:9000` generally accepts traffic on all available interfaces. Do not expose the unauthenticated collector directly to an untrusted network.

## Dashboard flags

```text
-database  SQLite path, default measurements.db
-address   HTTP bind address, default :8080
```

Examples:

```bash
# Local-only dashboard
go run ./cmd/dashboard -address 127.0.0.1:8080

# Trusted LAN dashboard
go run ./cmd/dashboard \
  -address :8080 \
  -database "$HOME/.local/share/edgelens/measurements.db"
```

There is no authentication or TLS. Keep it on loopback, a protected lab network, or behind a secured reverse proxy.

## Running across multiple machines

Assume the collector host is `192.168.1.20`.

On the collector host:

```bash
mkdir -p "$HOME/.local/share/edgelens"

go run ./cmd/collector \
  -udp-address 192.168.1.20:9000 \
  -database "$HOME/.local/share/edgelens/measurements.db"
```

In another terminal on that host:

```bash
go run ./cmd/dashboard \
  -address 127.0.0.1:8080 \
  -database "$HOME/.local/share/edgelens/measurements.db"
```

On the Linux agent host:

```bash
go run ./cmd/agent \
  -collector 192.168.1.20:9000 \
  -host compute-node-01 \
  -disk-device nvme0n1 \
  -disk-mount / \
  -network-interface enp1s0 \
  -report-interval 5s
```

Permit UDP port `9000` only from trusted agents. For example, an appropriately scoped UFW rule may be needed. `ping` proves basic IP reachability but does not prove that UDP port `9000` is open or that the collector is running.

Cross-host timestamps are not corrected by EdgeLens. Use NTP/PTP appropriate to the lab if host timelines must be compared. HFT and tightly coupled HPC environments may require substantially stronger clock discipline than ordinary NTP.

## What the dashboard looks like

The dashboard is an embedded single-page interface served by the dashboard binary. No separate frontend build is required.

### Header and navigation

The top area shows the EdgeLens name, a short subtitle, and a host selector. Below it are two tabs:

- **Host telemetry**
- **Experiments**

The visual style is a restrained light operational dashboard with bordered chart panels, compact labels, monospace numeric values, teal host/process trends, orange navigation emphasis, and status badges.

### Host telemetry view

The host view shows the selected host's latest samples as line charts:

- CPU usage
- Memory usage percentage
- Memory used bytes
- Memory total bytes
- Swap usage
- Disk-space usage
- Disk read bytes/second
- Disk write bytes/second
- Network receive bytes/second
- Network send bytes/second
- Temperature

The status line reports the number of samples, host, and newest timestamp. Empty databases show an explicit no-data state.

### Experiments view

The experiment selector labels runs with their start time, command, and status. Selecting a run loads:

- Status badge: `running`, `completed`, `failed`, or `interrupted`.
- Exact command and argument vector.
- Host and tracked workload PID.
- Start and finish times.
- Monotonic elapsed duration captured by the agent.
- Requested perf events and actual perf version.
- Failure reason when present.
- Raw machine-readable `perf stat` evidence.
- Flame chart when a `flame-folded` artifact exists.
- Go heap summary when a `heap-summary` artifact exists.
- Process timeline charts for CPU, RSS, data/heap mapping, reads, writes, threads, minor faults, and major faults.

Flame rectangles are proportional to sampled stack counts. Hovering a rectangle shows the symbol and sample count. When all distinct stacks do not fit within the artifact limit, EdgeLens keeps the highest-count stacks and accounts for all omitted samples in an explicit `[other]` frame.

## Interpreting the evidence

### Host CPU

Host CPU usage is calculated from two aggregate `/proc/stat` observations:

$$
CPU_{host} = 100 \times \frac{\Delta total - \Delta idle}{\Delta total}
$$

This describes aggregate machine utilization over the report interval. It does not identify which process consumed the CPU.

### Process CPU

Process CPU combines user and system ticks from `/proc/<pid>/stat` and computes an interval rate:

$$
CPU_{process} = 100 \times \frac{\Delta ticks}{ticks\_per\_second \times \Delta seconds}
$$

The first process sample establishes a baseline and therefore reports zero rates. A multithreaded process can exceed `100%` because its CPU ticks may advance concurrently on multiple cores.

### Memory

- **RSS** is resident physical memory reported as `VmRSS`.
- **Virtual memory** is the process virtual address space reported as `VmSize`.
- **Data/heap mapping** is `VmData` from `/proc/<pid>/status`.

`VmData` is not an object-level heap measurement. It can include writable data mappings in addition to allocator-managed heap pages. Use the Go pprof summary for Go allocation-site attribution.

### I/O

The process reader records cumulative `read_bytes` and `write_bytes` from `/proc/<pid>/io`, then computes bytes/second from consecutive observations. Counter decreases are treated as zero delta rather than producing impossible negative rates.

### Page faults

- A **minor fault** resolves without storage I/O, commonly by mapping an already available page.
- A **major fault** requires storage-backed page-in work and is usually more expensive.

Fault counts alone do not establish a bottleneck. Correlate them with elapsed time, RSS growth, I/O rates, and workload phase.

### Perf counters

Useful relationships include:

$$
IPC = \frac{instructions}{cycles}
$$

$$
branch\ miss\ rate = \frac{branch\ misses}{branches}
$$

$$
cache\ miss\ rate = \frac{cache\ misses}{cache\ references}
$$

Low IPC can result from memory latency, dependency chains, branch recovery, frontend starvation, frequency behavior, or counter multiplexing. Treat counter values as observations, not diagnoses.

### Flame charts

A wide frame accumulated many samples. It does not necessarily mean the function is inefficient; it may simply perform most of the intended work. Flame charts are statistical samples, not a complete trace, and short functions can be missed.

### Heap summaries

The current heap artifact uses pprof's `inuse_space` sample index. It emphasizes memory retained at profile time, not total allocation churn. For allocation-rate investigation, a future artifact could intentionally select `alloc_space`; those are different questions.

## Architecture and data flow

### Continuous mode

1. The agent gathers CPU, disk I/O, and network rates concurrently over the report interval.
2. It gathers memory, disk-space, and temperature observations.
3. `wire.FromSnapshot` creates the stable v1 measurement.
4. `JSONCodec.Encode` serializes flat legacy JSON.
5. `UDPSender` sends one datagram to the collector.
6. The collector discriminates the packet as a measurement.
7. SQLite inserts it using `(host, timestamp)` as the idempotency key.
8. The dashboard reads recent measurements and renders host charts.

### Experiment mode

1. The agent validates run arguments and capture limits.
2. The experiment runner performs `perf` preflight. Failure prevents workload launch.
3. The perf adapter starts a process group containing perf and the explicit workload.
4. The runner returns immediately with a running record and a receive-only outcome channel.
5. The agent emits `run_started` and begins host/process sampling.
6. A wait goroutine reaps the perf/workload tree and prepares bounded evidence.
7. Optional heap analysis runs after workload exit.
8. Each artifact is sent as an independent idempotent `artifact` event.
9. The agent emits exactly one `run_finished` event when connectivity permits.
10. The dashboard queries normalized run, timeline, and artifact records.

## Important design choices

### The agent launches workloads

The dashboard is read-only. Launching from the agent keeps execution authority on the observed Linux host and prevents a browser/API endpoint from becoming a remote command service.

### No shell parsing

`-run-command` and repeatable `-run-arg` flags preserve the exact `argv` boundary. This avoids quoting ambiguity and shell injection. Empty individual arguments remain legal because an empty argument is different from a missing argument.

### Fail-closed perf preflight

EdgeLens checks the perf executable, version, requested counters, kernel policy context, and optional stack sampling before starting the workload. A blocked profiler does not silently produce an unprofiled run.

### Stable telemetry protocol plus separate experiment protocol

The original measurement schema remains version `1` and flat JSON. Experiment events have their own `schema_version`, kind discriminator, message ID, run ID, host, and millisecond timestamp. This avoids breaking cheap continuous telemetry when richer experiment evidence evolves.

### Start is synchronous; completion is asynchronous

Process creation is synchronous so the agent never reports `running` before the OS confirms launch and provides a PID. `Wait` runs in a goroutine and sends exactly one outcome through a channel buffered to capacity one.

The one-element buffer lets the wait goroutine publish its sole result without requiring the receiver to be ready at the same instant. It does not guarantee that the caller will consume the value.

### UTC timestamps and monotonic duration

Persisted timestamps use UTC for correlation. Elapsed duration is measured from `time.Now` values while they still carry Go's process-local monotonic clock reading. Serializing `time.Time` removes that monotonic component, so duration is persisted separately.

### Process-group cancellation

The perf wrapper receives its own process group. Context cancellation sends `SIGTERM` to the group; Go's `WaitDelay` provides a bounded escalation path. The child is always paired with `Wait` so exited processes are reaped.

### Procfs counters before rates

CPU and I/O sources are cumulative kernel counters. The reader retains one prior observation per PID and derives rates across real elapsed time. This is more defensible than labeling a single counter read as an instantaneous rate.

### Bounded independent artifacts

Each evidence artifact is a separate event with:

- UTF-8 text
- artifact kind
- SHA-256 checksum
- original byte count
- maximum text size of `48 KiB`

Every encoded experiment packet is capped at `60 KiB`. Raw perf-stat text is rejected if oversized. Flame text is already a derived aggregation, so high-count stacks are retained and omitted samples are represented explicitly as `[other]` rather than silently discarded.

Independent artifact events avoid forcing several large evidence objects into one UDP datagram.

### Idempotent SQLite writes

UDP can duplicate packets. Primary keys and `ON CONFLICT DO NOTHING` make duplicate starts, samples, and artifacts harmless. Terminal state can move from `running` to one terminal status exactly once; a repeated matching finish is harmless, while a conflicting terminal overwrite is rejected.

### Dependency injection at OS boundaries

Perf preflight accepts a command runner, and experiment orchestration accepts a capture backend. Tests can exercise failure, ordering, and concurrency without requiring privileged counters or real child processes.

## Wire protocol

### Legacy measurement

The v1 measurement remains flat JSON and uses Unix seconds. Representative fields:

```json
{
  "v": 1,
  "host": "compute-01",
  "ts": 1787431800,
  "cpu_pct": 62.4,
  "mem_used_pct": 48.2,
  "disk_read_bps": 1048576,
  "net_recv_bps": 524288,
  "temp_c": 58.5
}
```

### Experiment event kinds

| Kind | Payload | Meaning |
|---|---|---|
| `run_started` | command, args, PID, capture spec | Workload launch was confirmed. |
| `process_sample` | CPU, memory, I/O, threads, faults, state | One procfs observation and derived rates. |
| `artifact` | kind, text, checksum, byte count | One bounded evidence object. |
| `run_finished` | terminal status, duration, exit/signal, reason | Exactly one terminal outcome when delivery succeeds. |

Every experiment event includes:

```json
{
  "schema_version": 1,
  "message_id": "128-bit-hex-value",
  "kind": "process_sample",
  "run_id": "openssl-sha256",
  "host": "compute-01",
  "event_at_ms": 1787431800123
}
```

The decoder rejects unsupported schema versions, missing identities, invalid payload combinations, negative rates, invalid terminal states, malformed artifacts, checksum mismatches, and oversized packets before persistence.

## SQLite storage

The collector creates schema additively. It does not rewrite the legacy measurements table.

### `measurements`

Stores continuous host telemetry. Primary key:

```text
(host, timestamp)
```

Timestamps remain Unix seconds for v1 compatibility.

### `experiment_runs`

Stores one lifecycle row per run:

- run ID and host
- executable and JSON argument vector
- `running`, `completed`, `failed`, or `interrupted` status
- start/finish milliseconds and elapsed nanoseconds
- child PID, exit code, signal, and failure reason
- JSON capture specification and perf version

Primary key: `run_id`.

### `process_samples`

Stores process timeline values at millisecond precision. Primary key:

```text
(run_id, sampled_at_ms)
```

### `artifacts`

Stores bounded text, SHA-256, and byte count. Primary key:

```text
(run_id, artifact_kind)
```

### Indexes

- Runs by `(host, started_at_ms DESC)`.
- Samples by `(run_id, sampled_at_ms)`.
- Artifacts by `run_id`.

SQLite foreign keys are enabled and the store limits itself to one open connection. The collector and dashboard are separate processes accessing the same embedded database file; SQLite is not itself a network database.

## HTTP API

All APIs are read-only.

### Measurements

```http
GET /api/measurements?limit=720
```

- Default limit: `720`
- Allowed range: `1..5000`
- Response: oldest-to-newest subset of the newest measurements

Example:

```bash
curl -s 'http://127.0.0.1:8080/api/measurements?limit=10'
```

### Run list

```http
GET /api/runs?host=compute-01&limit=50
```

- Default limit: `50`
- HTTP maximum: `500`
- Optional exact host filter
- Newest runs first

### Run detail

```http
GET /api/runs/{id}
```

Example:

```bash
curl -s http://127.0.0.1:8080/api/runs/openssl-sha256
```

### Process samples

```http
GET /api/runs/{id}/process-samples?limit=2000
```

- Default limit: `2000`
- Maximum: `10000`
- Ordered by sample time ascending

### Artifact

```http
GET /api/runs/{id}/artifacts/{kind}
```

Allowed kinds:

- `perf-stat`
- `flame-folded`
- `heap-summary`

Example:

```bash
curl -s \
  http://127.0.0.1:8080/api/runs/openssl-sha256/artifacts/perf-stat
```

Invalid limits and paths return `400`; absent runs/artifacts return `404`; database failures return `500`.

## Repository file guide

### Root files

| File | Responsibility |
|---|---|
| [`README.md`](README.md) | Operator, architecture, and development guide. |
| [`go.mod`](go.mod) | Module identity, Go version, and direct dependencies. |
| [`go.sum`](go.sum) | Dependency checksums used by the Go module system. |
| [`measurements.db`](measurements.db) | Local runtime/demo SQLite database. It is application data, not source code. |
| [`untitled:plan-edgeLens.prompt.md`](untitled:plan-edgeLens.prompt.md) | Original planning document and scope history. Runtime code does not read it. |

### Commands

| File | Responsibility |
|---|---|
| [`cmd/agent/main.go`](cmd/agent/main.go) | Parses agent flags; selects continuous or experiment mode; resolves host identity; handles SIGINT/SIGTERM; runs host telemetry; sends measurement, sample, artifact, and lifecycle packets. |
| [`cmd/collector/main.go`](cmd/collector/main.go) | Binds UDP; decodes discriminated packets; routes each packet kind to the matching idempotent store method. |
| [`cmd/dashboard/main.go`](cmd/dashboard/main.go) | Opens SQLite, binds HTTP, embeds the dashboard handler, and prints the usable URL. |

### Dashboard

| File | Responsibility |
|---|---|
| [`internal/dashboard/server.go`](internal/dashboard/server.go) | Serves embedded HTML and implements bounded read-only measurement/run/sample/artifact APIs. |
| [`internal/dashboard/server_test.go`](internal/dashboard/server_test.go) | Tests legacy measurement behavior, experiment response shapes, missing records, and limit validation. |
| [`internal/dashboard/web/index.html`](internal/dashboard/web/index.html) | Complete embedded UI: tabs, host selector, line charts, run selector, metadata, evidence text, flame rendering, and process timeline. |

### Experiment domain and orchestration

| File | Responsibility |
|---|---|
| [`internal/experiment/model.go`](internal/experiment/model.go) | Defines the validated running-run record and capture specification. Defensively copies argument/event slices to preserve launch provenance. |
| [`internal/experiment/model_test.go`](internal/experiment/model_test.go) | Tests valid construction, invalid identities/PIDs/capture settings, legal empty arguments, UTC storage, and defensive copying. |
| [`internal/experiment/runner.go`](internal/experiment/runner.go) | Coordinates preflight, perf session launch, asynchronous completion, optional heap analysis, and the one-result outcome channel. Defines injectable capture interfaces. |
| [`internal/experiment/runner_test.go`](internal/experiment/runner_test.go) | Uses a fake backend/session to prove nonblocking return, exactly-one completion, outcome contents, and fail-before-launch behavior. |

### Perf capture

| File | Responsibility |
|---|---|
| [`internal/perf/stat.go`](internal/perf/stat.go) | Finds and preflights perf; launches stat/record sessions; owns process-group cancellation; locates the workload beneath perf wrappers; reads bounded stat output; runs perf script; folds stacks; bounds flame evidence; invokes Go pprof; cleans temporary files. |
| [`internal/perf/stat_test.go`](internal/perf/stat_test.go) | Tests fail-closed preflight with a fake runner, event validation, descendant PID discovery, strict output bounds, folded-stack aggregation, `[other]` accounting, and missing heap profiles. |

### Process metrics

| File | Responsibility |
|---|---|
| [`internal/procmetrics/process.go`](internal/procmetrics/process.go) | Reads `/proc/<pid>/stat`, `status`, and `io`; parses command names safely; records cumulative counters; computes interval CPU and I/O rates; identifies normal process disappearance. |
| [`internal/procmetrics/process_test.go`](internal/procmetrics/process_test.go) | Uses temporary procfs fixtures to test parsing, rate math, command names containing spaces/parentheses, malformed input, and disappeared PIDs. |

### Host metric aggregation

| File | Responsibility |
|---|---|
| [`internal/metricagg/config.go`](internal/metricagg/config.go) | Defines source names and minimum/default report intervals. |
| [`internal/metricagg/aggregator.go`](internal/metricagg/aggregator.go) | Collects CPU, memory, disk, network, and temperature into one typed system snapshot; parallelizes independent rate observations. |
| [`internal/metricagg/aggregator_test.go`](internal/metricagg/aggregator_test.go) | Tests invalid/subsecond report-interval rejection. |

### Linux host metric readers

| File | Responsibility |
|---|---|
| [`internal/sysmetrics/cpu.go`](internal/sysmetrics/cpu.go) | Parses aggregate jiffies from `/proc/stat` and derives host CPU usage over an interval. |
| [`internal/sysmetrics/mem.go`](internal/sysmetrics/mem.go) | Parses `/proc/meminfo`; derives memory and swap usage using `MemAvailable`. |
| [`internal/sysmetrics/disk.go`](internal/sysmetrics/disk.go) | Uses `statfs` for capacity and `/proc/diskstats` deltas for read/write throughput. |
| [`internal/sysmetrics/network.go`](internal/sysmetrics/network.go) | Parses `/proc/net/dev` and derives receive/send throughput. |
| [`internal/sysmetrics/temp.go`](internal/sysmetrics/temp.go) | Reads the first available `/sys/class/thermal/thermal_zone*` source. |

### Transport and codecs

| File | Responsibility |
|---|---|
| [`internal/transport/udp.go`](internal/transport/udp.go) | Connected UDP sender with short-write detection. |
| [`internal/transport/receiver.go`](internal/transport/receiver.go) | UDP listener and datagram receive wrapper. |
| [`internal/transport/codec/json.go`](internal/transport/codec/json.go) | Preserves flat v1 measurement JSON; detects experiment packets through `schema_version`; validates event payloads and packet size before persistence. |
| [`internal/transport/codec/json_test.go`](internal/transport/codec/json_test.go) | Tests v1 compatibility, all experiment kinds, invalid schemas/IDs/checksums, and packet bounds. |

### Wire contracts

| File | Responsibility |
|---|---|
| [`internal/wire/measurement.go`](internal/wire/measurement.go) | Stable v1 host-measurement contract and the only snapshot-to-wire field mapping. |
| [`internal/wire/experiment.go`](internal/wire/experiment.go) | Separately versioned event envelope, payload structs, artifact hashing, event-kind validation, and UDP size limits. |

### SQLite store

| File | Responsibility |
|---|---|
| [`internal/store/sqlite.go`](internal/store/sqlite.go) | Opens SQLite; applies additive schema; writes idempotent measurements/runs/samples/artifacts; atomically finalizes runs; reads dashboard records; verifies artifact integrity on read. |
| [`internal/store/sqlite_test.go`](internal/store/sqlite_test.go) | Tests legacy measurements, experiment redelivery, lifecycle transitions, timelines, artifact checksums, and required indexes. |

## Testing and verification

Run the complete suite:

```bash
go test ./...
```

Run static analysis:

```bash
go vet ./...
```

Build the production commands:

```bash
go build ./cmd/agent ./cmd/collector ./cmd/dashboard
```

Run focused tests during development:

```bash
go test ./internal/experiment
go test ./internal/perf
go test ./internal/procmetrics
go test ./internal/transport/codec
go test ./internal/store
go test ./internal/dashboard
```

The race detector is also valuable on supported Linux architectures:

```bash
go test -race ./internal/experiment ./internal/procmetrics ./internal/transport/codec
```

Some ARM64 environments cannot start ThreadSanitizer because their virtual-address-space layout is incompatible. That is an environment limitation, not a passing race result.

## Build and deployment

Build local binaries:

```bash
go build -o edgelens-agent ./cmd/agent
go build -o edgelens-collector ./cmd/collector
go build -o edgelens-dashboard ./cmd/dashboard
```

Cross-compile for common Linux targets:

```bash
GOOS=linux GOARCH=arm64 go build -o edgelens-agent-arm64 ./cmd/agent
GOOS=linux GOARCH=amd64 go build -o edgelens-agent-amd64 ./cmd/agent
GOOS=linux GOARCH=amd64 go build -o edgelens-collector-amd64 ./cmd/collector
GOOS=linux GOARCH=amd64 go build -o edgelens-dashboard-amd64 ./cmd/dashboard
```

Check the target with `uname -m`. `aarch64` normally maps to `GOARCH=arm64`; `x86_64` maps to `GOARCH=amd64`. A 32-bit Raspberry Pi OS may require `GOARCH=arm`.

The agent binary still requires a compatible external `perf` executable at runtime.

## Troubleshooting

### `interface ... not found in /proc/net/dev`

The configured interface is not visible in the agent's network namespace.

```bash
cat /proc/net/dev
ip link
```

Use the exact visible name with `-network-interface`.

### Disk device cannot be measured

Use the kernel block-device name, not a mount path:

```bash
lsblk
cat /proc/diskstats
```

Use `-disk-device nvme0n1` and separately use `-disk-mount /`, for example.

### Perf preflight fails

Run the same command as the same user:

```bash
cat /proc/sys/kernel/perf_event_paranoid
perf stat -e cycles,instructions -- true
```

Possible causes include kernel policy, unsupported PMU events, a mismatched perf package, VM/container restrictions, or missing capabilities. Prefer a narrowly configured lab policy over running the full agent as root.

### Flame chart is empty or symbols are unknown

- Confirm `perf record` works for the user.
- Run a workload long enough to collect samples.
- Build with debug symbols and usable frame information.
- Check kernel symbol restrictions when kernel frames matter.
- Remember that very short functions may not be statistically sampled.

### No process samples appear

Very short workloads may exit before the first useful procfs read. EdgeLens takes an immediate baseline, but reading `stat`, `status`, and `io` is not atomic; the process can exit between files. The run and perf evidence can still be valid without a timeline.

### Heap summary is missing

- Verify that the workload actually wrote the file named by `-heap-profile`.
- Verify that it is a Go pprof profile, not an unrelated heap format.
- Run `go tool pprof -top -sample_index=inuse_space PROFILE` manually.
- Ensure the agent can execute the `go` command.

### Runs or artifacts are intermittently missing

UDP can lose and reorder packets. If `run_started` is lost, the collector rejects later samples/artifacts for the unknown run. If an artifact packet is lost, the run can finish without that artifact. Current EdgeLens does not retry or acknowledge packets.

For dependable remote labs, the next transport improvement should be acknowledged control events and authenticated streaming artifact upload, not larger UDP packets.

### Dashboard shows no data

- Confirm collector and dashboard use the exact same database path.
- Confirm the collector logs stored packets.
- Query `GET /api/measurements` and `GET /api/runs` directly.
- Check that the dashboard's host selector is not filtering to another host.

### `database is locked`

Use one collector writer and point the dashboard at the same local file. Do not place SQLite on an unreliable network filesystem. Long external SQLite transactions can contend with EdgeLens.

## Security and operational cautions

- The agent intentionally executes a user-specified program. Restrict who can invoke it and under which OS account.
- The dashboard does not launch commands.
- Collector UDP and dashboard HTTP are unauthenticated and unencrypted.
- Artifact text and command arguments may contain sensitive paths or symbols.
- Do not expose either service directly to the public internet.
- Do not assume a successful UDP send means the collector persisted the packet.
- Do not raise artifact limits beyond UDP-safe sizes; use a streaming transport for larger evidence.
- Apply an external database retention/backup policy. EdgeLens does not delete old data automatically.

## Evidence versus interpretation

EdgeLens stores measured facts and bounded derived summaries:

- Kernel and procfs counters
- Host/process rates calculated across observation windows
- Perf counter output
- Statistical call stacks
- Go pprof summary text

An explanation such as “the workload is memory-bound” or “branch prediction caused the slowdown” requires comparison, controls, and engineering judgment. For credible performance work:

1. Keep command, arguments, input, binary, compiler flags, host, kernel, and perf event set stable.
2. Warm caches intentionally or explicitly measure cold behavior.
3. Repeat runs and examine variance.
4. Control CPU affinity, frequency scaling, NUMA placement, and competing workloads when they matter.
5. Compare counter ratios and timelines with sampled code locations.
6. Treat telemetry as evidence, not an automated causal report.
