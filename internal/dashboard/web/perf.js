(() => {
  "use strict";

  const counterInfo = Object.freeze({
    "task-clock": ["CPU execution time", "Total time the workload actually ran on a CPU. This is CPU time, not the experiment's wall-clock duration."],
    "cpu-clock": ["CPU execution time", "Software-measured CPU time consumed by the workload, distinct from wall-clock duration."],
    "cycles": ["CPU cycles", "Processor clock cycles spent running the workload. Use with instructions to understand CPU efficiency."],
    "instructions": ["Instructions", "Machine instructions retired by the CPU. Perf may also report instructions per cycle (IPC)."],
    "branches": ["Branches", "Branch instructions executed by the workload."],
    "branch-instructions": ["Branches", "Branch instructions executed by the workload."],
    "branch-misses": ["Branch misses", "Branches the CPU predicted incorrectly. A high miss percentage can waste pipeline work."],
    "cache-references": ["Cache references", "Last-level cache accesses observed for the workload."],
    "cache-misses": ["Cache misses", "Last-level cache accesses that missed. A high percentage can indicate poor memory locality."],
    "context-switches": ["Context switches", "Times Linux switched execution to or from this workload. User-space-only counting can omit kernel scheduler activity."],
    "cpu-migrations": ["CPU migrations", "Times Linux moved the workload between CPU cores. User-space-only counting can omit kernel scheduler activity."],
    "page-faults": ["Page mappings", "Times Linux had to map a memory page on demand. This combines minor and major faults; it is not an error or leak count."],
    "minor-faults": ["Minor page faults", "Page mappings resolved without reading data from storage."],
    "major-faults": ["Major page faults", "Page mappings that required storage I/O and may cause visible stalls."]
  });

  function parseStat(text) {
    if (!text) return [];
    return text.split("\n").flatMap(line => {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) return [];
      const fields = line.split(",").map(field => field.trim());
      if (fields.length < 3 || !fields[2]) return [];
      const event = fields[2];
      const modifierMatch = event.match(/:([ukh]+)$/i);
      return [{
        rawValue: fields[0],
        unit: fields[1],
        event,
        name: event.replace(/:([ukh]+)$/i, ""),
        modifiers: modifierMatch?.[1].toLowerCase() || "",
        enabledPercent: Number(fields[4]),
        metricValue: fields[5],
        metricUnit: fields.slice(6).join(", ")
      }];
    });
  }

  function scope(modifiers) {
    const scopes = [];
    if (modifiers.includes("u")) scopes.push("user space");
    if (modifiers.includes("k")) scopes.push("kernel");
    if (modifiers.includes("h")) scopes.push("hypervisor");
    return scopes.join(" + ") || "all scopes";
  }

  function formatValue(rawValue, unit, eventName) {
    if (!rawValue || rawValue.startsWith("<")) return rawValue || "Unavailable";
    const value = Number(rawValue.replaceAll(" ", ""));
    if (!Number.isFinite(value)) return rawValue;
    if ((eventName === "task-clock" || eventName === "cpu-clock") && !unit) {
      const milliseconds = value / 1e6;
      return milliseconds >= 1000 ? `${(milliseconds / 1000).toFixed(3)} s` : `${milliseconds.toFixed(2)} ms`;
    }
    if (unit === "msec") return value >= 1000 ? `${(value / 1000).toFixed(3)} s` : `${value.toFixed(2)} ms`;
    if (unit) return `${value.toLocaleString(undefined, { maximumFractionDigits: 3 })} ${unit}`;
    return new Intl.NumberFormat(undefined, {
      notation: Math.abs(value) >= 100000 ? "compact" : "standard",
      maximumFractionDigits: 2
    }).format(value);
  }

  function formatMetric(valueText, unit) {
    const value = Number(valueText);
    if (!Number.isFinite(value) || !unit) return "";
    const formatted = value.toLocaleString(undefined, { maximumFractionDigits: 3 });
    if (unit === "CPUs utilized") return `${value.toFixed(2)} average CPU cores busy`;
    if (unit === "/sec") return `${formatted} events per second`;
    if (unit.toLowerCase() === "k/sec") return `${formatted} thousand events per second`;
    if (unit === "insn per cycle") return `${formatted} instructions per CPU cycle`;
    return `${formatted} ${unit}`;
  }

  globalThis.EdgeLensPerf = Object.freeze({ counterInfo, parseStat, scope, formatValue, formatMetric });
})();