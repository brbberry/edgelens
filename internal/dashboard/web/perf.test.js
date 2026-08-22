"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

require("./perf.js");

const { parseStat, scope, formatValue, formatMetric } = globalThis.EdgeLensPerf;

test("parses perf CSV and ignores comments", () => {
  const counters = parseStat([
    "# started on Sat Aug 22 14:37:56 2026",
    "802130000,,task-clock:u,802130000,100.00,1.001,CPUs utilized",
    "17469,,page-faults:u,802130000,100.00,21.778,K/sec"
  ].join("\n"));

  assert.equal(counters.length, 2);
  assert.deepEqual(counters[0], {
    rawValue: "802130000",
    unit: "",
    event: "task-clock:u",
    name: "task-clock",
    modifiers: "u",
    enabledPercent: 100,
    metricValue: "1.001",
    metricUnit: "CPUs utilized"
  });
});

test("formats Pi and standard task-clock values as durations", () => {
  assert.equal(formatValue("802130000", "", "task-clock"), "802.13 ms");
  assert.equal(formatValue("802.13", "msec", "task-clock"), "802.13 ms");
  assert.equal(formatValue("16149", "msec", "task-clock"), "16.149 s");
});

test("translates perf metrics and scopes into plain language", () => {
  assert.equal(scope("u"), "user space");
  assert.equal(scope("uk"), "user space + kernel");
  assert.equal(formatMetric("1.001", "CPUs utilized"), "1.00 average CPU cores busy");
  assert.equal(formatMetric("21.778", "K/sec"), "21.778 thousand events per second");
});