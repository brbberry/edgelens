## Plan: Linux Perf Experiment Runs

Extend EdgeLens into a Linux performance-lab first slice by adding an agent-owned experiment lifecycle. The agent will preflight `perf stat`, launch an explicit child command, sample that child from `/proc` at the existing report interval while it continues host telemetry, and send bounded perf-output evidence plus lifecycle events to the collector. The collector will persist normalized run, process-sample, and artifact metadata in SQLite; the dashboard will expose read-only investigation APIs. The stable v1 host-measurement contract remains compatible and high-volume tracing/GPU tooling stays out of this slice.

**Steps**
1. **Define the experiment domain and protocol boundary**
   - Create `/home/bberry39/projects/edgelens/internal/experiment/model.go` for the shared immutable domain records: `Run`, `RunStatus`, `ProcessSample`, and `Artifact`. Include run ID, host, command/arguments, monotonic/UTC start and finish timestamps, child PID, process exit state, requested capture spec, and failure reason.
   - Keep `/home/bberry39/projects/edgelens/internal/wire/measurement.go` and `wire.Version` unchanged for continuous host telemetry. Add a separately versioned wire envelope/event model in `/home/bberry39/projects/edgelens/internal/wire/experiment.go`, with explicit message kinds for run-started, process-sample, and run-finished/artifact delivery.
   - Include a schema/version value, message ID, run ID, host, event timestamp, and a bounded artifact payload representation. Perf output must be UTF-8 text, have a configured maximum byte size, and include SHA-256 plus original byte length; oversized output is rejected before transmission and recorded as a run failure.
   - Refactor `/home/bberry39/projects/edgelens/internal/transport/codec/codec.go` and `/home/bberry39/projects/edgelens/internal/transport/codec/json.go` from a measurement-only decoder to a discriminated packet decoder that retains v1 measurement compatibility and validates each experiment event before persistence. Update `/home/bberry39/projects/edgelens/cmd/collector/main.go` to route decoded packets to the matching store method.

2. **Implement the Linux performance-capture adapter**
   - Create `/home/bberry39/projects/edgelens/internal/perf/stat.go` around an injected command runner. It validates that `perf` exists and can run the requested counter set before starting the workload, then executes `perf stat` only for the child process using machine-readable output and a conservative default counter set (task-clock, cycles, instructions, branches, branch-misses, cache-references, cache-misses, context-switches, CPU-migrations, page-faults).
   - Make privilege/availability checks explicit: if `perf_event_paranoid`, permissions, unsupported events, or executable lookup prevent the preflight, return a typed capture-preflight error and do not launch the workload, per the agreed fail-closed behavior.
   - Keep the adapter’s artifact destination abstract via an `ArtifactSink` interface. Implement a UDP-compatible bounded text sink first; define, but do not implement, the HTTP sink contract so later transfer changes affect only this adapter layer.
   - Record the exact perf executable version, requested events, parsed status, raw bounded output, checksum, and capture error in the finished-run data. Do not parse summary values into columns in this first slice; raw artifact plus capture spec is the source of truth.

3. **Add `/proc` process attribution**
   - Create `/home/bberry39/projects/edgelens/internal/procmetrics/process.go` to read the launched PID’s CPU time, RSS, aggregate read/write bytes, thread count, and process state from procfs. Capture elapsed CPU ticks and I/O counters so rates are calculated across sample intervals rather than represented as misleading instantaneous values.
   - Design the collector with a `ProcessReader` interface and a clock dependency so procfs parsing and rate calculations are unit-testable from fixtures without needing a real child process.
   - Treat process exit between samples as normal lifecycle behavior. The agent stops sampling after reaping the child, writes the final process observation when available, and publishes the finish event exactly once.

4. **Extend the agent with an explicit run mode**
   - Refactor `/home/bberry39/projects/edgelens/cmd/agent/main.go` only enough to separate its existing continuous telemetry loop from an experiment-run path. Preserve its current flag behavior when run flags are absent.
   - Add flags for command execution and capture configuration: `-run-command`, repeatable or delimiter-safe command arguments, `-run-id` (generated if omitted), `-artifact-max-bytes`, and `-perf-events`. Validate that a workload command is supplied only in run mode and reject unsafe ambiguous shell-string execution; use `exec.CommandContext` with an argument vector, never a shell.
   - In run mode: preflight perf; generate/publish a run-started event; launch the child under perf; take process samples synchronized to each telemetry report interval; send host measurements through the unchanged v1 path; publish samples and a single terminal run event containing perf evidence; propagate workload/capture outcomes through status and exit code.
   - Ensure cancellation and SIGINT/SIGTERM terminate and reap the workload and perf subprocesses, then publish an interrupted terminal status where collector connectivity allows it.

5. **Persist evidence with additive SQLite migrations**
   - Extend `/home/bberry39/projects/edgelens/internal/store/sqlite.go` with idempotent additive schema setup for `experiment_runs`, `process_samples`, and `artifacts`; do not mutate or rewrite the existing `measurements` table.
   - Use a text run ID primary key. Store all event times at millisecond precision in the new tables, while leaving existing second-resolution measurements intact. Use `(run_id, sampled_at_ms)` for process sample uniqueness and `(run_id, artifact_kind)` for artifact idempotency.
   - Add indexes for the dashboard queries: runs by `(host, started_at_ms DESC)`, process samples by `(run_id, sampled_at_ms)`, and artifacts by `run_id`.
   - Add store methods that are safe under UDP redelivery: create-or-ignore run start, insert-or-ignore process sample, finalize a run atomically without overwriting an already-terminal status, persist a verified bounded artifact, list runs, read a run detail, and read its process timeline. Reject unknown run IDs and invalid terminal transitions.

6. **Expose a focused investigation API and dashboard view**
   - Extend `/home/bberry39/projects/edgelens/internal/dashboard/server.go` with read-only routes: `GET /api/runs`, `GET /api/runs/{id}`, `GET /api/runs/{id}/process-samples`, and `GET /api/runs/{id}/artifacts/{kind}`. Reuse the current query validation style and cap list/timeline limits; return no artifact data until the requested run and artifact ID are validated.
   - Update `/home/bberry39/projects/edgelens/internal/dashboard/web/index.html` to add a compact experiment selector and run detail panel. Show command, host/PID, lifecycle timestamps/status, perf capture metadata/output, and process CPU/RSS/read/write/thread charts aligned with host observations. Keep run creation in the agent CLI, not the dashboard.
   - Preserve the existing host-metric dashboard and `/api/measurements` behavior. New evidence APIs are additive.

7. **Document the experiment workflow and operating constraints**
   - Update `/home/bberry39/projects/edgelens/README.md` with a Linux-only perf-lab workflow: collector/dashboard startup, agent run invocation, expected artifacts, locating `perf_event_paranoid`, conservative privilege guidance, data retention/size behavior, and an example workload command.
   - State the evidence distinction clearly: host/process telemetry and perf counters are measured facts; any explanation requires a report or user interpretation. Document that UDP delivery is best-effort and artifacts are intentionally bounded.

**Relevant files**
- `/home/bberry39/projects/edgelens/cmd/agent/main.go` — existing host-telemetry loop; add isolated experiment-run lifecycle without breaking normal collection.
- `/home/bberry39/projects/edgelens/cmd/collector/main.go` — dispatch the new experiment packet kinds to storage.
- `/home/bberry39/projects/edgelens/internal/wire/measurement.go` — preserve the v1 host measurement contract.
- `/home/bberry39/projects/edgelens/internal/transport/codec/codec.go` and `/home/bberry39/projects/edgelens/internal/transport/codec/json.go` — evolve decoding to route a discriminated packet while preserving v1 interoperability.
- `/home/bberry39/projects/edgelens/internal/store/sqlite.go` — additive evidence tables, idempotent writes, queries, and indexes.
- `/home/bberry39/projects/edgelens/internal/dashboard/server.go` and `/home/bberry39/projects/edgelens/internal/dashboard/web/index.html` — additive, read-only run investigation surface.
- New `/home/bberry39/projects/edgelens/internal/experiment/`, `/home/bberry39/projects/edgelens/internal/perf/`, and `/home/bberry39/projects/edgelens/internal/procmetrics/` packages — domain model, capture adapter, and Linux process reader.

**Verification**
1. Add packet codec tests proving legacy v1 `Measurement` JSON still round-trips, each experiment event decodes to the intended kind, invalid schema/message IDs are rejected, and artifact bounds/checksums are enforced.
2. Add perf-adapter tests with a fake runner for executable unavailable, permission-denied preflight, successful stat output, child nonzero exit, cancellation, and oversize artifact cases. Assert no child-launch call follows a failed preflight.
3. Add procfs fixture tests for stat/status/io parsing, rate calculation across two samples, malformed input, and disappeared PID behavior.
4. Extend `/home/bberry39/projects/edgelens/internal/store/sqlite_test.go` for migrations against an existing measurements database, UDP-redelivery idempotency, valid/invalid lifecycle transitions, timestamp ordering, artifact checksum persistence, and new indexes’ query paths.
5. Extend `/home/bberry39/projects/edgelens/internal/dashboard/server_test.go` for response shapes, limit/path validation, missing run/artifact responses, and preservation of `/api/measurements` behavior.
6. Run `go test ./...`, `go vet ./...`, and `go build ./cmd/agent ./cmd/collector ./cmd/dashboard` on Linux. Manually run `perf stat true` as the same user, then launch a bounded CPU workload through the agent and verify one completed run, an artifact checksum match, aligned process samples, and unchanged continuous host charts.

**Decisions**
- First milestone targets kernel investigation on Linux only; macOS remains collector/dashboard-only in this release.
- The agent, not the dashboard, launches an explicit command. Dashboard remains a read-only investigation surface.
- Default kernel evidence is `perf stat`, not `perf record`, eBPF, or a trace. Capture preflight is fail-closed: a blocked/unavailable perf environment prevents workload launch.
- The agent samples the launched PID for CPU, RSS, I/O, and thread count at the telemetry interval.
- Start with bounded UDP artifact transfer, behind an `ArtifactSink` interface shaped to allow a later HTTP implementation.
- Existing v1 host telemetry remains a compatible, cheap continuous stream. Experiments travel in new packet kinds and persist in separate tables.

**Scope boundaries**
- Included: Linux child-command lifecycle, perf-stat evidence, procfs process samples, UDP-bounded artifact delivery, SQLite metadata/timeline persistence, read-only dashboard inspection, tests, and docs.
- Deliberately excluded: arbitrary shell evaluation, dashboard-run controls, automatic anomaly triggers, `perf record`/flamegraphs, eBPF/BCC, GPU vendor APIs/CUDA/ROCm/Metal metrics, hardware-topology discovery, cross-host clock synchronization, HTTP artifact upload implementation, and automated causal reports. These can follow once the first evidence chain is dependable.

**Further considerations**
1. The new command-vector CLI must settle an ergonomic flag representation before implementation. Recommended: `-run-command /path/to/program` plus repeatable `-run-arg value`, avoiding shell parsing and delimiter edge cases.
2. `perf_event_paranoid` and kernel capabilities vary materially between the PC and Pi. The README should require a local capability check and capture its result in every run.
3. When larger profiles/traces arrive, implement an HTTP `ArtifactSink` with per-run authorization and streaming limits rather than attempting to raise UDP payload limits.
