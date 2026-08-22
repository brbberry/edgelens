package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/brbberry/edgelens/internal/experiment"
	"github.com/brbberry/edgelens/internal/metricagg"
	perfadapter "github.com/brbberry/edgelens/internal/perf"
	"github.com/brbberry/edgelens/internal/procmetrics"
	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/transport/codec"
	"github.com/brbberry/edgelens/internal/wire"
)

func main() {
	os.Exit(run())
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func run() int {
	sources := metricagg.DefaultMetricSources()
	destination := flag.String("collector", "127.0.0.1:9000", "UDP address of the collector")
	hostOverride := flag.String("host", "", "host identity to include in measurements (defaults to the system hostname)")
	reportInterval := flag.Duration("report-interval", metricagg.DefaultReportInterval, "interval covered by each rate measurement and between reports")
	diskDevice := flag.String("disk-device", sources.DiskDevice, "disk device to measure")
	diskMountPath := flag.String("disk-mount", sources.DiskMountPath, "mount path whose disk usage to measure")
	networkInterface := flag.String("network-interface", sources.NetworkInterface, "network interface to measure")
	runCommand := flag.String("run-command", "", "explicit workload executable; enables experiment mode")
	runID := flag.String("run-id", "", "experiment ID (generated when omitted)")
	artifactMaxBytes := flag.Int64("artifact-max-bytes", 32*1024, "maximum bytes for each text artifact")
	perfEventsText := flag.String("perf-events", strings.Join(perfadapter.DefaultEvents, ","), "comma-separated perf counter names")
	collectFlame := flag.Bool("flamegraph", false, "collect sampled call stacks with perf record")
	heapProfilePath := flag.String("heap-profile", "", "Go pprof heap profile written by the workload to analyze after exit")
	var runArgs stringList
	flag.Var(&runArgs, "run-arg", "workload argument; repeat for each argument")
	flag.Parse()

	if *reportInterval < metricagg.MinimumReportInterval {
		fmt.Printf("report-interval must be at least %s\n", metricagg.MinimumReportInterval)
		return 2
	}
	if *artifactMaxBytes <= 0 || *artifactMaxBytes > wire.MaxArtifactBytes {
		fmt.Printf("artifact-max-bytes must be between 1 and %d\n", wire.MaxArtifactBytes)
		return 2
	}
	if *runCommand == "" && (len(runArgs) > 0 || *runID != "" || *collectFlame || *heapProfilePath != "") {
		fmt.Println("run arguments, run ID, flamegraph, and heap profile require -run-command")
		return 2
	}
	sources.DiskDevice = *diskDevice
	sources.DiskMountPath = *diskMountPath
	sources.NetworkInterface = *networkInterface

	host := *hostOverride
	if host == "" {
		var err error
		host, err = os.Hostname()
		if err != nil {
			fmt.Println("get hostname:", err)
			return 1
		}
	}
	sender, err := transport.NewUDPSender(*destination)
	if err != nil {
		fmt.Println("create UDP sender:", err)
		return 1
	}
	defer sender.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := codec.JSONCodec{}
	if *runCommand == "" {
		fmt.Printf("sending measurements from %s to %s every %s\n", host, *destination, *reportInterval)
		telemetryLoop(ctx, sender, encoder, host, *reportInterval, sources)
		return 0
	}

	id := *runID
	if id == "" {
		id = newID()
	}
	events := splitEvents(*perfEventsText)
	if len(events) == 0 {
		fmt.Println("perf-events must contain at least one event")
		return 2
	}
	return runExperiment(ctx, sender, encoder, sources, experimentConfig{
		ID: id, Host: host, Command: *runCommand, Args: runArgs,
		Interval: *reportInterval, Events: events, ArtifactMaxBytes: *artifactMaxBytes,
		CollectFlame: *collectFlame, HeapProfilePath: *heapProfilePath,
	})
}

type experimentConfig struct {
	ID, Host, Command string
	Args              []string
	Interval          time.Duration
	Events            []string
	ArtifactMaxBytes  int64
	CollectFlame      bool
	HeapProfilePath   string
}

func runExperiment(ctx context.Context, sender *transport.UDPSender, encoder codec.JSONCodec, sources metricagg.MetricSources, config experimentConfig) int {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	hostTelemetryDone := make(chan struct{})
	go func() {
		defer close(hostTelemetryDone)
		telemetryLoop(runContext, sender, encoder, config.Host, config.Interval, sources)
	}()

	capture := experiment.CaptureSpec{PerfEvents: config.Events, ByteLimit: config.ArtifactMaxBytes}
	running, outcomes, err := experiment.StartRun(runContext, experiment.OSCaptureBackend{}, config.ID, config.Host,
		config.Command, config.Args, capture, config.CollectFlame, config.HeapProfilePath)
	if err != nil {
		cancel()
		<-hostTelemetryDone
		fmt.Println("start experiment:", err)
		return 1
	}
	startedEvent := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: newID(), Kind: wire.PacketRunStarted,
		RunID: running.ID, Host: running.Host, EventAtMS: running.StartTime.UnixMilli(),
		Started: &wire.RunStartedPayload{
			Command: running.Command, Args: running.Args, StartedAtMS: running.StartTime.UnixMilli(), ChildPID: running.ChildPID,
			Capture: wire.CaptureSpec{PerfEvents: config.Events, ArtifactMaxBytes: config.ArtifactMaxBytes,
				CollectFlame: config.CollectFlame, HeapProfilePath: config.HeapProfilePath},
		},
	}
	if err := sendExperiment(sender, encoder, startedEvent); err != nil {
		fmt.Println("send run-started event:", err)
	}

	reader := procmetrics.NewReader()
	sampleProcess(sender, encoder, reader, running.ID, running.Host, running.ChildPID)
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	var outcome experiment.ProcessOutcome
	for {
		select {
		case <-ticker.C:
			sampleProcess(sender, encoder, reader, running.ID, running.Host, running.ChildPID)
		case received, ok := <-outcomes:
			if !ok {
				continue
			}
			outcome = received
			goto finished
		}
	}

finished:
	cancel()
	<-hostTelemetryDone
	status := "completed"
	failureReason := ""
	if ctx.Err() != nil {
		status = "interrupted"
		failureReason = ctx.Err().Error()
	} else if outcome.Err != nil || outcome.ExitCode == nil || *outcome.ExitCode != 0 {
		status = "failed"
		if outcome.Err != nil {
			failureReason = outcome.Err.Error()
		} else {
			failureReason = "workload exited unsuccessfully"
		}
	}
	artifacts := make([]wire.ArtifactPayload, 0, 3)
	if outcome.PerfStat != "" {
		artifacts = append(artifacts, wire.NewTextArtifact("perf-stat", outcome.PerfStat))
	}
	if outcome.FoldedStacks != "" {
		artifacts = append(artifacts, wire.NewTextArtifact("flame-folded", outcome.FoldedStacks))
	}
	if outcome.HeapSummary != "" {
		artifacts = append(artifacts, wire.NewTextArtifact("heap-summary", outcome.HeapSummary))
	}
	for index := range artifacts {
		artifactEvent := wire.ExperimentEvent{
			SchemaVersion: wire.ExperimentVersion, MessageID: newID(), Kind: wire.PacketArtifact,
			RunID: running.ID, Host: running.Host, EventAtMS: outcome.FinishedAt.UnixMilli(), Artifact: &artifacts[index],
		}
		if err := sendExperiment(sender, encoder, artifactEvent); err != nil {
			status = "failed"
			failureReason = fmt.Sprintf("send %s artifact: %v", artifacts[index].Kind, err)
			fmt.Println(failureReason)
		}
	}
	finishedEvent := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: newID(), Kind: wire.PacketRunFinished,
		RunID: running.ID, Host: running.Host, EventAtMS: outcome.FinishedAt.UnixMilli(),
		Finished: &wire.RunFinishedPayload{
			Status: status, FinishedAtMS: outcome.FinishedAt.UnixMilli(), ElapsedNS: outcome.Elapsed.Nanoseconds(),
			ExitCode: outcome.ExitCode, Signal: outcome.Signal, FailureReason: failureReason,
			PerfVersion: outcome.PerfVersion,
		},
	}
	if err := sendExperiment(sender, encoder, finishedEvent); err != nil {
		fmt.Println("send run-finished event:", err)
	}
	if status != "completed" {
		fmt.Printf("experiment %s finished as %s: %s\n", config.ID, status, failureReason)
		return 1
	}
	fmt.Printf("experiment %s completed in %s\n", config.ID, outcome.Elapsed)
	return 0
}

func sampleProcess(sender *transport.UDPSender, encoder codec.JSONCodec, reader *procmetrics.Reader, runID, host string, pid int) {
	sample, err := reader.Read(pid)
	if err != nil {
		if !strings.Contains(err.Error(), procmetrics.ErrProcessExited.Error()) {
			fmt.Println("sample process:", err)
		}
		return
	}
	event := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: newID(), Kind: wire.PacketProcessSample,
		RunID: runID, Host: host, EventAtMS: sample.SampledAt.UnixMilli(),
		Sample: &wire.ProcessSamplePayload{
			SampledAtMS: sample.SampledAt.UnixMilli(), CPUTicks: sample.CPUTicks, CPUPercent: sample.CPUPercent,
			RSSBytes: sample.RSSBytes, VirtualBytes: sample.VirtualBytes, HeapDataBytes: sample.HeapDataBytes,
			ReadBytes: sample.ReadBytes, WriteBytes: sample.WriteBytes, ReadBPS: sample.ReadBPS, WriteBPS: sample.WriteBPS,
			ThreadCount: sample.ThreadCount, MinorFaults: sample.MinorFaults, MajorFaults: sample.MajorFaults, ProcessState: sample.State,
		},
	}
	if err := sendExperiment(sender, encoder, event); err != nil {
		fmt.Println("send process sample:", err)
	}
}

func telemetryLoop(ctx context.Context, sender *transport.UDPSender, encoder codec.JSONCodec, host string, interval time.Duration, sources metricagg.MetricSources) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		cycleStartedAt := time.Now()
		snapshot, err := metricagg.GatherSystemSnapshot(interval, sources)
		if err != nil {
			fmt.Println("gather system snapshot:", err)
		} else {
			measurement := wire.FromSnapshot(snapshot, host, time.Now().Unix())
			payload, err := encoder.Encode(measurement)
			if err != nil {
				fmt.Println("encode measurement:", err)
			} else if err := sender.Send(payload); err != nil {
				fmt.Println("send measurement:", err)
			}
		}
		if delay := time.Until(cycleStartedAt.Add(interval)); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
	}
}

func sendExperiment(sender *transport.UDPSender, encoder codec.JSONCodec, event wire.ExperimentEvent) error {
	payload, err := encoder.EncodePacket(wire.Packet{Kind: event.Kind, Experiment: &event})
	if err != nil {
		return err
	}
	return sender.Send(payload)
}

func splitEvents(text string) []string {
	parts := strings.Split(text, ",")
	events := make([]string, 0, len(parts))
	for _, part := range parts {
		if event := strings.TrimSpace(part); event != "" {
			events = append(events, event)
		}
	}
	return events
}

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}
