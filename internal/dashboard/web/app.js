(() => {
  "use strict";

  const { counterInfo, parseStat, scope: perfScope, formatValue: formatPerfValue, formatMetric: formatPerfMetric } = globalThis.EdgeLensPerf;
  const metrics = [
    ["cpu_pct", "CPU usage", "%"], ["mem_used_pct", "Memory usage", "%"], ["mem_used_b", "Memory used", "B"],
    ["mem_total_b", "Memory total", "B"], ["swap_used_pct", "Swap usage", "%"], ["disk_used_pct", "Disk usage", "%"],
    ["disk_read_bps", "Disk read", "B/s"], ["disk_write_bps", "Disk write", "B/s"], ["net_recv_bps", "Network received", "B/s"],
    ["net_sent_bps", "Network sent", "B/s"], ["temp_c", "Temperature", "C"]
  ];
  const processMetrics = [
    ["cpu_percent", "Process CPU", "%"], ["rss_bytes", "Resident memory", "B"], ["heap_data_bytes", "Data/heap mapping", "B"],
    ["read_bps", "Process reads", "B/s"], ["write_bps", "Process writes", "B/s"], ["thread_count", "Threads", "count"],
    ["minor_faults", "Minor page faults", "count"], ["major_faults", "Major page faults", "count"]
  ];

  function element(id) {
    const found = document.getElementById(id);
    if (!found) throw new Error(`Missing dashboard element #${id}`);
    return found;
  }

  const hostSelect = element("host");
  const chartGrid = element("charts");
  const status = element("status");
  const runSelect = element("run-select");
  const runStatus = element("run-status");
  const runContent = element("run-content");
  const runFacts = element("run-facts");
  const processCharts = element("process-charts");
  const perfCounters = element("perf-counters");
  const perfCount = element("perf-count");
  const perfSummary = element("perf-summary");
  const perfNotes = element("perf-notes");
  const perfRaw = element("perf-raw");
  const perfOutput = element("perf-output");
  const heapOutput = element("heap-output");
  const flame = element("flame");
  let measurements = [];

  function format(value, unit) {
    if (unit === "B" || unit === "B/s") {
      const units = ["B", "KB", "MB", "GB", "TB"];
      let index = 0;
      while (Math.abs(value) >= 1024 && index < units.length - 1) { value /= 1024; index++; }
      return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[index]}${unit === "B/s" ? "/s" : ""}`;
    }
    if (unit === "count") return Math.round(value).toLocaleString();
    return `${value.toFixed(1)}${unit === "%" ? "%" : " C"}`;
  }

  function chart(metric, records) {
    const [field, label, unit] = metric;
    const values = records.map(record => Number(record[field]));
    const minimum = Math.min(...values);
    const maximum = Math.max(...values);
    const padding = (maximum - minimum) * 0.1 || Math.max(Math.abs(maximum) * 0.1, 1);
    const low = minimum - padding;
    const high = maximum + padding;
    const range = high - low;
    const width = 300, height = 156, left = 46, right = 8, top = 12, bottom = 28;
    const firstTimestamp = records[0].ts;
    const lastTimestamp = records.at(-1).ts;
    const timeRange = lastTimestamp - firstTimestamp;
    const plotHeight = height - top - bottom;
    const plotWidth = width - left - right;
    const points = records.map((record, index) => {
      const value = values[index];
      const x = left + plotWidth * (timeRange ? (record.ts - firstTimestamp) / timeRange : .5);
      const y = top + (height - top - bottom) * (1 - (value - low) / range);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    }).join(" ");
    const firstTime = new Date(firstTimestamp * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    const lastTime = new Date(lastTimestamp * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    const middleY = top + plotHeight / 2;
    const bottomY = height - bottom;
    return `<article class="chart"><div class="chart-head"><h2>${label}</h2><span class="value">${format(values.at(-1), unit)}</span></div><svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${label} trend"><line class="grid-line" x1="${left}" y1="${top}" x2="${width - right}" y2="${top}"/><line class="grid-line" x1="${left}" y1="${middleY}" x2="${width - right}" y2="${middleY}"/><line class="grid-line" x1="${left}" y1="${bottomY}" x2="${width - right}" y2="${bottomY}"/><line class="axis-line" x1="${left}" y1="${top}" x2="${left}" y2="${bottomY}"/><line class="axis-line" x1="${left}" y1="${bottomY}" x2="${width - right}" y2="${bottomY}"/><polyline class="trend" points="${points}"/><text class="axis-label" x="${left - 5}" y="${top + 3}" text-anchor="end">${format(high, unit)}</text><text class="axis-label" x="${left - 5}" y="${middleY + 3}" text-anchor="end">${format((low + high) / 2, unit)}</text><text class="axis-label" x="${left - 5}" y="${bottomY + 3}" text-anchor="end">${format(low, unit)}</text><text class="axis-label" x="${left}" y="${height - 5}">${firstTime}</text><text class="axis-label" x="${width - right}" y="${height - 5}" text-anchor="end">${lastTime}</text></svg></article>`;
  }

  function renderMeasurements() {
    const records = measurements.filter(record => record.host === hostSelect.value);
    if (!records.length) {
      chartGrid.innerHTML = '<div class="empty">No measurements available for this host yet.</div>';
      status.textContent = "No host telemetry is available for this selection.";
      return;
    }
    chartGrid.innerHTML = metrics.map(metric => chart(metric, records)).join("");
    const latest = records.at(-1);
    status.textContent = `${records.length} samples from ${latest.host} through ${new Date(latest.ts * 1000).toLocaleString()}.`;
  }

  function populateSelect(select, values, value, label) {
    select.replaceChildren(...values.map(item => {
      const option = document.createElement("option");
      option.value = value(item);
      option.textContent = label(item);
      return option;
    }));
  }

  function fact(label, value) {
    const term = document.createElement("dt");
    term.textContent = label;
    const detail = document.createElement("dd");
    if (label === "Status") {
      const badge = document.createElement("span");
      badge.className = `badge ${value}`;
      badge.textContent = value;
      detail.append(badge);
    } else {
      detail.textContent = value;
    }
    runFacts.append(term, detail);
  }

  async function fetchJSON(path) {
    const response = await fetch(path);
    if (!response.ok) throw new Error(`Request failed (${response.status})`);
    return response.json();
  }

  async function optionalArtifact(runID, kind) {
    const response = await fetch(`/api/runs/${encodeURIComponent(runID)}/artifacts/${kind}`);
    if (response.status === 404) return null;
    if (!response.ok) throw new Error(`Unable to load ${kind} artifact`);
    return response.json();
  }

  function renderPerf(text, hasFlameProfile) {
    perfOutput.textContent = text || "No perf-stat artifact.";
    perfRaw.hidden = !text;
    perfRaw.open = false;
    perfNotes.replaceChildren();
    perfNotes.hidden = true;
    const counters = parseStat(text);
    perfCounters.replaceChildren();
    perfCount.textContent = counters.length ? `${counters.length} counters` : "";
    if (!counters.length) {
      perfSummary.textContent = "No perf counter artifact is available for this run.";
      const empty = document.createElement("div");
      empty.className = "empty";
      empty.textContent = "No counters recorded.";
      perfCounters.append(empty);
      return;
    }

    const insights = [];
    const taskClock = counters.find(counter => counter.name === "task-clock");
    const utilization = Number(taskClock?.metricValue);
    if (Number.isFinite(utilization) && taskClock.metricUnit.includes("CPUs utilized")) insights.push(`${utilization.toFixed(2)} average CPU cores busy`);
    const instructions = counters.find(counter => counter.name === "instructions");
    const ipc = Number(instructions?.metricValue);
    if (Number.isFinite(ipc) && instructions.metricUnit.includes("insn per cycle")) insights.push(`${ipc.toFixed(2)} instructions per CPU cycle`);
    perfSummary.textContent = `${insights.length ? `${insights.join(" · ")}. ` : ""}Large numbers are abbreviated; rates and ratios are calculated by Linux perf.`;

    const notes = [];
    if (hasFlameProfile) notes.push("This run also recorded stacks, so these totals include some profiling overhead. Use a counter-only run for cleaner performance comparisons.");
    if (counters.some(counter => perfScope(counter.modifiers) === "user space")) notes.push("USER SPACE means kernel activity was excluded by this host's perf permissions. Scheduler counters such as switches and migrations may therefore be incomplete.");
    for (const note of notes) {
      const paragraph = document.createElement("p");
      paragraph.textContent = note;
      perfNotes.append(paragraph);
    }
    perfNotes.hidden = !notes.length;

    for (const counter of counters) {
      const unavailable = !counter.rawValue || counter.rawValue.startsWith("<");
      const [label, description] = counterInfo[counter.name] || [counter.name, "A Linux perf event selected for this experiment."];
      const article = document.createElement("article");
      article.className = `perf-counter${unavailable ? " unavailable" : ""}`;
      const heading = document.createElement("div");
      heading.className = "perf-counter-head";
      const name = document.createElement("span");
      name.className = "perf-counter-name";
      name.textContent = label;
      const scope = document.createElement("span");
      scope.className = "perf-scope";
      scope.textContent = perfScope(counter.modifiers);
      scope.title = "Execution privilege levels included in this counter";
      heading.append(name, scope);
      const valueLabel = document.createElement("span");
      valueLabel.className = "perf-value-label";
      valueLabel.textContent = "Measured total";
      const value = document.createElement("strong");
      value.className = "perf-counter-value";
      value.textContent = formatPerfValue(counter.rawValue, counter.unit, counter.name);
      value.title = counter.unit ? `${counter.rawValue} ${counter.unit}` : counter.rawValue;
      const derivedText = formatPerfMetric(counter.metricValue, counter.metricUnit);
      const derived = document.createElement("div");
      derived.className = "perf-derived";
      derived.textContent = derivedText ? `Perf calculation: ${derivedText}` : (Number.isFinite(counter.enabledPercent) && counter.enabledPercent < 100 ? `${counter.enabledPercent.toFixed(1)}% time enabled` : "");
      const explanation = document.createElement("p");
      explanation.className = "perf-description";
      explanation.textContent = description;
      article.append(heading, valueLabel, value, derived, explanation);
      perfCounters.append(article);
    }
  }

  function renderFlame(folded) {
    flame.replaceChildren();
    if (!folded) { flame.setAttribute("viewBox", "0 0 900 120"); return; }
    const root = { name: "all samples", count: 0, children: new Map() };
    let maximumDepth = 1;
    for (const line of folded.trim().split("\n")) {
      const split = line.lastIndexOf(" ");
      if (split < 1) continue;
      const count = Number(line.slice(split + 1));
      const stack = line.slice(0, split).split(";");
      if (!Number.isFinite(count) || count <= 0) continue;
      root.count += count;
      maximumDepth = Math.max(maximumDepth, stack.length);
      let node = root;
      for (const name of stack) {
        if (!node.children.has(name)) node.children.set(name, { name, count: 0, children: new Map() });
        node = node.children.get(name);
        node.count += count;
      }
    }
    const width = 900, rowHeight = 24, height = (maximumDepth + 1) * rowHeight;
    flame.setAttribute("viewBox", `0 0 ${width} ${height}`);
    const namespace = "http://www.w3.org/2000/svg";
    function color(name) { let hash = 0; for (const character of name) hash = (hash * 31 + character.charCodeAt(0)) | 0; return `hsl(${Math.abs(hash) % 330} 58% 68%)`; }
    function draw(node, x, y, nodeWidth) {
      const children = [...node.children.values()].sort((left, right) => right.count - left.count);
      let offset = x;
      for (const child of children) {
        const childWidth = nodeWidth * child.count / node.count;
        const rect = document.createElementNS(namespace, "rect");
        rect.setAttribute("x", offset);
        rect.setAttribute("y", height - y - rowHeight);
        rect.setAttribute("width", Math.max(0, childWidth - .5));
        rect.setAttribute("height", rowHeight - 1);
        rect.setAttribute("fill", color(child.name));
        const title = document.createElementNS(namespace, "title");
        title.textContent = `${child.name}: ${child.count} samples`;
        rect.append(title);
        flame.append(rect);
        if (childWidth > 72) {
          const text = document.createElementNS(namespace, "text");
          text.setAttribute("x", offset + 4);
          text.setAttribute("y", height - y - 8);
          text.setAttribute("class", "flame-label");
          text.textContent = child.name.slice(0, Math.floor(childWidth / 7));
          flame.append(text);
        }
        draw(child, offset, y + rowHeight, childWidth);
        offset += childWidth;
      }
    }
    if (root.count) draw(root, 0, 0, width);
  }

  async function renderRun() {
    const id = runSelect.value;
    if (!id) {
      runContent.hidden = true;
      runStatus.textContent = "No experiment runs recorded.";
      return;
    }
    runStatus.textContent = "Loading experiment evidence...";
    try {
      const [run, samples, perfArtifact, flameArtifact, heapArtifact] = await Promise.all([
        fetchJSON(`/api/runs/${encodeURIComponent(id)}`),
        fetchJSON(`/api/runs/${encodeURIComponent(id)}/process-samples?limit=10000`),
        optionalArtifact(id, "perf-stat"),
        optionalArtifact(id, "flame-folded"),
        optionalArtifact(id, "heap-summary")
      ]);
      runFacts.replaceChildren();
      fact("Status", run.status);
      fact("Command", [run.command, ...run.args].join(" "));
      fact("Host / PID", `${run.host} / ${run.child_pid}`);
      fact("Started", new Date(run.started_at_ms).toLocaleString());
      fact("Finished", run.finished_at_ms ? new Date(run.finished_at_ms).toLocaleString() : "running");
      fact("Elapsed", run.elapsed_ns == null ? "running" : `${(run.elapsed_ns / 1e9).toFixed(3)} s`);
      fact("Perf events", run.capture.perf_events.join(", "));
      fact("Perf version", run.perf_version || "not reported");
      if (run.failure_reason) fact("Failure", run.failure_reason);
      renderPerf(perfArtifact?.text || "", Boolean(flameArtifact?.text));
      heapOutput.textContent = heapArtifact?.text || "No Go heap profile artifact.";
      renderFlame(flameArtifact?.text || "");
      const timeline = samples.map(sample => ({ ...sample, ts: sample.sampled_at_ms / 1000 }));
      processCharts.innerHTML = timeline.length ? processMetrics.map(metric => chart(metric, timeline)).join("") : '<div class="empty">The process exited before a sample was captured.</div>';
      runStatus.textContent = `${samples.length} process samples. Artifacts are checksum-verified by the collector.`;
      runContent.hidden = false;
    } catch (error) {
      runStatus.textContent = error.message;
      runContent.hidden = true;
    }
  }

  async function load() {
    try {
      const [loadedMeasurements, runs] = await Promise.all([
        fetchJSON("/api/measurements?limit=720"),
        fetchJSON("/api/runs?limit=500")
      ]);
      measurements = loadedMeasurements;
      const hosts = [...new Set([...measurements.map(record => record.host), ...runs.map(run => run.host)])];
      populateSelect(hostSelect, hosts, host => host, host => host);
      populateSelect(runSelect, runs, run => run.id, run => `${new Date(run.started_at_ms).toLocaleString()} · ${run.command} · ${run.status}`);
      renderMeasurements();
      await renderRun();
    } catch (error) {
      status.textContent = error.message;
      chartGrid.innerHTML = '<div class="empty">The dashboard could not load telemetry.</div>';
    }
  }

  hostSelect.addEventListener("change", renderMeasurements);
  runSelect.addEventListener("change", renderRun);
  document.querySelectorAll(".tab").forEach(tab => tab.addEventListener("click", () => {
    document.querySelectorAll(".tab").forEach(item => {
      const active = item === tab;
      item.classList.toggle("active", active);
      item.setAttribute("aria-selected", active);
    });
    document.querySelectorAll(".view").forEach(view => { view.hidden = view.id !== tab.dataset.view; });
  }));
  void load();
})();