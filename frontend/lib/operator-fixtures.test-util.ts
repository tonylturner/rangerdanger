// Answers of the EU control-centre, field and firewall-session
// routes in the shapes the backend and the services really send
// (services/iec104/src/cc_*.c, services/devices, backend/internal/server),
// shared by the operator tests.

import type { PackageSummary } from "./api";
import type {
  CommandResult,
  DeviceAudit,
  BreakerState,
  RmuState,
  OltcState,
  FirewallSessionList,
  FieldState,
  OperatorJournal,
  OperatorPoint,
  OperatorState,
} from "./operator-api";

export const US_PACKAGE: PackageSummary = {
  id: "us-dnp3-substation",
  title: "US DNP3 Substation",
  revision: 3,
  active: true,
  operator_view: "us-dnp3-substation",
  policies: ["weak", "improved"],
};

export const EU_PACKAGE: PackageSummary = {
  id: "eu-iec104-substation",
  title: "EU IEC 104 Substation",
  revision: 1,
  active: true,
  operator_view: "eu-iec104",
  policies: ["weak", "improved"],
};

export const NOW = Date.parse("2026-10-09T12:00:30.000Z");

const GOOD = { iv: false, nt: false, sb: false, bl: false, ov: null };
const GOOD_MEASUREMENT = { ...GOOD, ov: false };

export const FRESH_POINTS: OperatorPoint[] = [
  {
    id: "feeder-breaker-position",
    ioa: 1001,
    asdu: "M_DP_TB_1",
    value: "closed",
    quality: GOOD,
    stale_reason: null,
    source_time: "2026-10-09T12:00:00.120Z",
    source_time_invalid: false,
    received_at: "2026-10-09T12:00:00.150Z",
    cot: 3,
  },
  {
    id: "feeder-protection-trip",
    ioa: 1002,
    asdu: "M_SP_TB_1",
    value: false,
    quality: GOOD,
    stale_reason: null,
    source_time: null,
    source_time_invalid: null,
    received_at: "2026-10-09T12:00:00.150Z",
    cot: 20,
  },
  {
    id: "feeder-current",
    ioa: 2001,
    asdu: "M_ME_NC_1",
    value: 28.93,
    quality: GOOD_MEASUREMENT,
    stale_reason: null,
    source_time: null,
    source_time_invalid: null,
    received_at: "2026-10-09T12:00:29.000Z",
    cot: 3,
  },
  {
    id: "feeder-voltage",
    ioa: 2002,
    asdu: "M_ME_NC_1",
    value: 19.94,
    quality: GOOD_MEASUREMENT,
    stale_reason: null,
    source_time: null,
    source_time_invalid: null,
    received_at: "2026-10-09T12:00:29.000Z",
    cot: 3,
  },
];

export const FRESH_STATE: OperatorState = {
  schema: 2,
  snapshot_seq: 120,
  rendered_at: "2026-10-09T12:00:29.900Z",
  cache_epoch: "a1b2c3d4",
  link: {
    state: "started",
    peer: "10.30.30.20:2404",
    since: "2026-10-09T11:59:58.000Z",
    last_rx_at: "2026-10-09T12:00:29.900Z",
    connection_seq: 3,
    gi: { state: "complete", started_at: "2026-10-09T11:59:58.100Z", completed_at: "2026-10-09T11:59:58.300Z" },
  },
  points: FRESH_POINTS,
  package_id: "eu-iec104-substation",
  range_generation: 7,
};

// The link was cut: every point keeps its value and times, IV+NT set.
export const STALE_STATE: OperatorState = {
  ...FRESH_STATE,
  link: {
    ...FRESH_STATE.link,
    state: "connecting",
    last_rx_at: "2026-10-09T12:00:09.900Z",
    gi: { state: "none", started_at: null, completed_at: null },
  },
  points: FRESH_POINTS.map((p) => ({
    ...p,
    quality: { ...p.quality, iv: true, nt: true },
    stale_reason: "link_lost",
  })),
};

export const JOURNAL: OperatorJournal = {
  cache_epoch: "a1b2c3d4",
  oldest_seq: 1,
  latest_seq: 4,
  entries: [
    { seq: 1, at: "2026-10-09T11:59:58.000Z", kind: "link_up", detail: { peer: "10.30.30.20:2404", connection_seq: 3 } },
    { seq: 2, at: "2026-10-09T11:59:58.300Z", kind: "gi_complete", detail: { connection_seq: 3, points_good: 4 } },
    { seq: 3, at: "2026-10-09T12:00:10.000Z", kind: "link_down", detail: { peer: "10.30.30.20:2404", connection_seq: 3, reason: "t1_timeout" } },
    { seq: 4, at: "2026-10-09T12:00:10.000Z", kind: "point_stale", detail: { point: "feeder-breaker-position", stale_reason: "link_lost", value: "closed" } },
  ],
};

// The CC before its first connect attempt settles.
export const EMPTY_JOURNAL: OperatorJournal = { cache_epoch: "a1b2c3d4", oldest_seq: null, latest_seq: null, entries: [] };

export function commandResult(result: CommandResult["result"]): CommandResult {
  return {
    id: "cmd-17", point: "feeder-breaker-command", operation: "open", result,
    reason: { completed: "fresh feedback matched", rejected: "execute rejected (negative ACT-CON)", failed: "feedback timeout (negative ACT-TERM)", unknown: "no ACT-CON within the command budget" }[result],
    selected_at: "2026-10-09T12:00:01.000Z",
    executed_at: "2026-10-09T12:00:01.100Z",
    con_at: result === "unknown" ? null : "2026-10-09T12:00:01.200Z",
    term_at: result === "completed" || result === "failed" ? "2026-10-09T12:00:01.700Z" : null,
    feedback: result === "completed" ? { ioa: 1001, value: "open", at: "2026-10-09T12:00:01.600Z" } : null,
  };
}

export const BREAKER: BreakerState = {
  schema: 2, kind: "breaker", device_id: "feeder-breaker", generation: 3141592, revision: 5,
  position: "open", remote_enabled: true, lockout: true, protection_tripped: true,
  event_seq: 9, changed_at: "2026-10-09T12:00:12.000Z",
  measurements: {
    current_a: 0, voltage_kv_ll: 0, source_kv_ll: 20, section_live: false, source_live: true, fault_current: false,
    valid: true, solver_epoch: "e1", solve_seq: 140,
    solved_at: "2026-10-09T12:00:29.500Z", received_at: "2026-10-09T12:00:29.510Z",
  },
};
export const RMU1: RmuState = {
  schema: 2, kind: "rmu", device_id: "rmu-1", generation: 1, revision: 3, event_seq: 31,
  changed_at: BREAKER.changed_at, remote_enabled: true, position: "closed", earth_closed: false, fault_passage: true,
  measurements: { ...BREAKER.measurements, source_kv_ll: 0 },
};
export const OLTC: OltcState = {
  schema: 2, kind: "oltc", device_id: "oltc-1", generation: 1, revision: 7, event_seq: 17,
  changed_at: BREAKER.changed_at, remote_enabled: true, tap: 1, target_tap: 2, moving: true,
  auto: false, blocked: false, armed: false, plc_request_result: "not_auto", heartbeat: 200,
  measurements: { ...BREAKER.measurements, voltage_kv_ll: 20.02, source_kv_ll: 110, section_live: true, source_live: true },
};
export const TRUTH: FieldState = {
  devices: {
    "breaker-1": BREAKER, "rmu-1": RMU1,
    "rmu-2": { ...RMU1, device_id: "rmu-2", position: "open", fault_passage: false, measurements: { ...RMU1.measurements, source_kv_ll: 19.97, source_live: true } },
    "oltc-1": OLTC,
  },
  process: {
    schema: 2, profile_id: "eu-cigre-mv-teaching", solver_epoch: "e1", solve_seq: 140,
    solved_at: "2026-10-09T12:00:29.500Z", truth_stale: false, fault_section: "S1", f2_tripped: false,
    load_multiplier: 1, source_pu: 1, tap: 1,
    buses: { bus20: { kv: 20.02, live: true }, s1: { kv: 0, live: false }, s2: { kv: 0, live: false }, s3: { kv: 19.97, live: true } },
    devices: Object.fromEntries([["feeder-breaker", 5], ["rmu-1", 3], ["rmu-2", 3], ["oltc-1", 7]].map(([id, revision]) => [id, { delivered: true, revision: Number(revision), solved_revision: Number(revision) }])),
  },
};

const CLOSED_DEVICE: BreakerState = {
  ...BREAKER, revision: 4, position: "closed", lockout: false, protection_tripped: false, event_seq: 8,
  changed_at: "2026-10-09T11:50:00.000Z",
};

export const AUDIT: DeviceAudit = {
  generation: 3141592,
  oldest_seq: 1,
  latest_seq: 3,
  entries: [
    {
      seq: 1,
      at: "2026-10-09T12:00:12.000Z",
      operation: "local_protection_trip",
      result: "accepted",
      before: CLOSED_DEVICE,
      after: BREAKER,
      context: { peer: "protection", protocol: "local-protection", zone: "field" },
      detail: { cause: "fault_current" },
    },
    {
      seq: 2,
      at: "2026-10-09T12:00:12.900Z",
      operation: "first_solve",
      result: "accepted",
      before: BREAKER,
      after: BREAKER,
      context: { peer: "10.50.50.20:40112", protocol: "process", zone: "process" },
      detail: { revision: 5, solver_epoch: "e1", solve_seq: 113, solved_at: "2026-10-09T12:00:12.800Z", current_a: 0 },
    },
    {
      seq: 3,
      at: "2026-10-09T12:00:13.500Z",
      operation: "close",
      result: "rejected",
      before: BREAKER,
      after: BREAKER,
      context: { peer: "10.30.30.20:41000", protocol: "modbus", zone: "station" },
      detail: "lockout",
    },
  ],
};

// GET /api/firewall/sessions?dport=2404: the CC's session and Kali's.
export const SESSIONS: FirewallSessionList = {
  count: 2,
  sessions: [
    { proto: "tcp", src: "10.60.60.20", dst: "10.30.30.20", sport: 40312, dport: 2404, state: "ESTABLISHED", timeout: 431999 },
    { proto: "tcp", src: "10.10.10.50", dst: "10.30.30.20", sport: 51000, dport: 2404, state: "ESTABLISHED", timeout: 431980 },
  ],
};

// Full schema-2 snapshot: all 31 monitored points in contract §5.
const EXTRA_POINTS: [string, number, OperatorPoint["value"], string][] = [
  ["rmu-1-position", 1101, "closed", "M_DP_TB_1"],
  ["rmu-1-remote", 1102, true, "M_SP_TB_1"],
  ["rmu-1-earth", 1103, false, "M_SP_TB_1"],
  ["rmu-1-fault-passage", 1104, false, "M_SP_TB_1"],
  ["rmu-1-current", 2101, 41.2, "M_ME_NC_1"],
  ["rmu-1-section-kv", 2102, 19.9, "M_ME_NC_1"],
  ["rmu-1-source-kv", 2103, 19.95, "M_ME_NC_1"],
  ["rmu-2-position", 1201, "open", "M_DP_TB_1"],
  ["rmu-2-remote", 1202, true, "M_SP_TB_1"],
  ["rmu-2-earth", 1203, false, "M_SP_TB_1"],
  ["rmu-2-fault-passage", 1204, false, "M_SP_TB_1"],
  ["rmu-2-current", 2201, 0, "M_ME_NC_1"],
  ["rmu-2-section-kv", 2202, 19.9, "M_ME_NC_1"],
  ["rmu-2-source-kv", 2203, 19.97, "M_ME_NC_1"],
  ["oltc-1-tap", 1301, { value: 0, transient: false }, "M_ST_TB_1"],
  ["oltc-1-remote", 1302, true, "M_SP_TB_1"],
  ["oltc-1-auto", 1303, true, "M_SP_TB_1"],
  ["oltc-1-at-lower", 1304, false, "M_SP_TB_1"],
  ["oltc-1-at-upper", 1305, false, "M_SP_TB_1"],
  ["oltc-1-blocked", 1306, false, "M_SP_TB_1"],
  ["oltc-1-bus-kv", 2301, 20.02, "M_ME_NC_1"],
  ["oltc-1-current", 2302, 100, "M_ME_NC_1"],
  ["plc-1-running", 1401, true, "M_SP_TB_1"],
  ["plc-1-input-valid", 1402, true, "M_SP_TB_1"],
  ["plc-1-setpoint-kv", 2401, 20, "M_ME_NC_1"],
  ["plc-1-deadband-pct", 2402, 1, "M_ME_NC_1"],
  ["plc-1-delay-s", 2403, 0, "M_ME_NC_1"],
];
export const FEEDER_STATE: OperatorState = {
  ...FRESH_STATE,
  points: [...FRESH_POINTS, ...EXTRA_POINTS.map(([id, ioa, value, asdu]) => ({
    ...FRESH_POINTS[0], id, ioa, value, asdu,
    quality: asdu === "M_ME_NC_1" ? GOOD_MEASUREMENT : GOOD,
    received_at: "2026-10-09T12:00:29.900Z",
  }))],
};
