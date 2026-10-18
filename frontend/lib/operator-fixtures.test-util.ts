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
  IndicationQuality,
  QdsQuality,
  OperatorState,
  StationClock,
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

const GOOD_INDICATION: IndicationQuality = { iv: false, nt: false, sb: false, bl: false, ov: null };
const GOOD_QDS: QdsQuality = { ...GOOD_INDICATION, ov: false };

export const FRESH_POINTS: OperatorPoint[] = [
  {
    device: "feeder-breaker", limits: null,
    id: "feeder-breaker-position",
    ioa: 1001,
    kind: "double-point", reporting: null,
    asdu: "M_DP_TB_1",
    value: "closed",
    quality: GOOD_INDICATION,
    stale_reason: null,
    source_time: "2026-10-09T12:00:00.120Z",
    source_time_invalid: false,
    received_at: "2026-10-09T12:00:00.150Z",
    cot: 3,
  },
  {
    device: "feeder-breaker", limits: null,
    id: "feeder-protection-trip",
    ioa: 1002,
    kind: "single-point", reporting: null,
    asdu: "M_SP_NA_1",
    value: false,
    quality: GOOD_INDICATION,
    stale_reason: null,
    source_time: null,
    source_time_invalid: null,
    received_at: "2026-10-09T12:00:00.150Z",
    cot: 20,
  },
  {
    device: "feeder-breaker", limits: null,
    id: "feeder-current",
    ioa: 2001,
    kind: "float", reporting: { cyclic_ms: 10000, deadband: 2 },
    asdu: "M_ME_NC_1",
    value: 28.93,
    quality: GOOD_QDS,
    stale_reason: null,
    source_time: null,
    source_time_invalid: null,
    received_at: "2026-10-09T12:00:29.000Z",
    cot: 3,
  },
  {
    device: "feeder-breaker", limits: { low: 19, high: 21 },
    id: "feeder-voltage",
    kind: "float", reporting: { cyclic_ms: 10000, deadband: 0.1 },
    ioa: 2002,
    asdu: "M_ME_NC_1",
    value: 19.94,
    quality: GOOD_QDS,
    stale_reason: null,
    source_time: null,
    source_time_invalid: null,
    received_at: "2026-10-09T12:00:29.000Z",
    cot: 3,
  },
];

export const FRESH_STATE: OperatorState = {
  schema: 3,
  snapshot_seq: 120,
  rendered_at: "2026-10-09T12:00:29.900Z",
  cache_epoch: "a1b2c3d4",
  link: {
    state: "started",
    peer: "10.30.30.20:2404",
    since: "2026-10-09T11:59:58.000Z",
    last_rx_at: "2026-10-09T12:00:29.900Z",
    connection_seq: 3,
    gi: { state: "complete", started_at: "2026-10-09T11:59:58.100Z", trigger: "connect", last_complete_at: "2026-10-09T11:59:58.300Z", next_due_at: "2026-10-09T12:14:58.100Z" },
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
    gi: { state: "none", started_at: null, trigger: null, last_complete_at: null, next_due_at: null },
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
    { seq: 2, at: "2026-10-09T11:59:58.300Z", kind: "gi_complete", detail: { connection_seq: 3, points_good: 4, trigger: "connect" } },
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
  schema: 3, kind: "breaker", device_id: "feeder-breaker", generation: 3141592, revision: 5,
  position: "open", remote_enabled: true, lockout: true, protection_tripped: true,
  event_seq: 9, changed_at: "2026-10-09T12:00:12.000Z",
  protection: { function: "overcurrent", pickup_a: 400, delay_ms: 1000, instantaneous_a: 3000, picked_up: false, picked_up_since: null, last_trip_current_a: 600 },
  measurements: {
    current_a: 0, voltage_kv_ll: 0, source_kv_ll: 20, section_live: false, source_live: true, p_mw: 0, q_mvar: 0, pf: null,
    valid: true, solver_epoch: "e1", solve_seq: 140,
    solved_at: "2026-10-09T12:00:29.500Z", received_at: "2026-10-09T12:00:29.510Z",
  },
};
export const RMU1: RmuState = {
  schema: 3, kind: "rmu", device_id: "rmu-1", generation: 1, revision: 3, event_seq: 31,
  changed_at: BREAKER.changed_at, remote_enabled: true, position: "closed", earth_closed: false, fault_passage: true,
  protection: { ...BREAKER.protection, function: "fpi", pickup_a: 300, delay_ms: 0, instantaneous_a: null },
  measurements: { ...BREAKER.measurements, source_kv_ll: 0 },
};
export const OLTC: OltcState = {
  schema: 3, kind: "oltc", device_id: "oltc-1", generation: 1, revision: 7, event_seq: 17,
  changed_at: BREAKER.changed_at, remote_enabled: true, tap: 1, target_tap: 2, moving: true,
  auto: false, blocked: false, armed: false, plc_request_result: "not_auto", heartbeat: 200,
  protection: { ...BREAKER.protection, function: "oltc_block", pickup_a: 866, delay_ms: 0, instantaneous_a: null },
  measurements: { ...BREAKER.measurements, voltage_kv_ll: 20.02, source_kv_ll: 110, section_live: true, source_live: true },
};
export const CLOCK: StationClock = {
  actual_time: "2026-10-09T12:00:29.500Z",
  reported_time: "2026-10-09T12:00:32.000Z",
  sync_status: "locked", time_offset_sec: 2.5, satellite_count: 9,
  holdover_started: "", holdover_drift_ppm: 0,
  ntp_enabled: true, ptp_enabled: false, irig_b_output: true,
  alarm: false, comms_ok: true, last_command_source: "lab",
};
export const TRUTH: FieldState = {
  clock: CLOCK,
  devices: {
    "breaker-1": BREAKER, "rmu-1": RMU1,
    "rmu-2": { ...RMU1, device_id: "rmu-2", position: "open", fault_passage: false, measurements: { ...RMU1.measurements, source_kv_ll: 19.97, source_live: true } },
    "oltc-1": OLTC,
  },
  process: {
    schema: 3, profile_id: "eu-cigre-mv-teaching", solver_epoch: "e1", solve_seq: 140,
    solved_at: "2026-10-09T12:00:29.500Z", truth_stale: false, fault_section: "S1", f2_tripped: false,
    load_multiplier: 1, source_pu: 1, tap: 1,
    profile: { enabled: true, load_factors: { residential: 0.8, commercial: 0.9, industrial: 1 }, source_pu: 1.01 },
    f2_protection: { picked_up: false, picked_up_since: null, last_trip_current_a: null },
    buses: { bus20: { kv: 20.02, live: true }, s1: { kv: 0, live: false }, s2: { kv: 0, live: false }, s3: { kv: 19.97, live: true } },
    devices: Object.fromEntries([["feeder-breaker", 5], ["rmu-1", 3], ["rmu-2", 3], ["oltc-1", 7]].map(([id, revision]) => [id, { delivered: true, current_a: 0, p_mw: 0, q_mvar: 0, revision: Number(revision), solved_revision: Number(revision) }])),
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
      detail: { cause: "overcurrent" },
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

// Full schema-3 snapshot: all 38 monitored points in contract §5.
const EXTRA_POINTS: [string, number, OperatorPoint["value"], string][] = [
  ["feeder-p-mw", 2003, 2.05, "M_ME_NC_1"],
  ["feeder-q-mvar", 2004, 0.62, "M_ME_NC_1"],
  ["feeder-pf", 2005, 0.957, "M_ME_NC_1"],
  ["oltc-1-p-mw", 2303, 3.05, "M_ME_NC_1"],
  ["oltc-1-q-mvar", 2304, 0.92, "M_ME_NC_1"],
  ["oltc-1-pf", 2305, 0.957, "M_ME_NC_1"],
  ["oltc-1-overcurrent-block", 1307, false, "M_SP_TB_1"],
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
  ["plc-1-setpoint-kv", 2401, 20.4, "M_ME_NC_1"],
  ["plc-1-deadband-pct", 2402, 1, "M_ME_NC_1"],
  ["plc-1-delay-s", 2403, 30, "M_ME_NC_1"],
];
export const FEEDER_STATE: OperatorState = {
  ...FRESH_STATE,
  points: [...FRESH_POINTS, ...EXTRA_POINTS.map(([id, ioa, value, asdu]): OperatorPoint => ({
    ...FRESH_POINTS[0], id, ioa, value, asdu,
    device: id.startsWith("feeder-") ? "feeder-breaker" : id.slice(0, id.startsWith("oltc-") ? 6 : 5),
    limits: id === "oltc-1-bus-kv" ? { low: 19.8, high: 21 } : id.includes("-kv") && id.startsWith("rmu-") ? { low: 19, high: 21 } : null,
    kind: asdu === "M_ME_NC_1" ? "float" : asdu === "M_ST_TB_1" ? "step-position" : asdu === "M_DP_TB_1" ? "double-point" : "single-point",
    reporting: asdu === "M_ME_NC_1" ? { cyclic_ms: id.startsWith("plc-") ? 60000 : 10000, deadband: id.startsWith("plc-") ? 0 : id.endsWith("current") ? 2 : id.endsWith("-pf") ? 0.01 : id.endsWith("-mw") || id.endsWith("-mvar") ? 0.05 : 0.1 } : null,
    source_time: asdu === "M_ME_NC_1" ? null : FRESH_POINTS[0].source_time,
    source_time_invalid: asdu === "M_ME_NC_1" ? null : false,
    quality: asdu === "M_ME_NC_1" || asdu === "M_ST_TB_1" ? GOOD_QDS : GOOD_INDICATION,
    received_at: "2026-10-09T12:00:29.900Z",
  }))],
};

// Contract rev 2 §2: all six worked states, without compatibility fields.
export const BOOT_STATE: OperatorState = {
  ...FEEDER_STATE,
  link: { ...FEEDER_STATE.link, state: "connecting", since: null, last_rx_at: null, gi: { state: "none", trigger: null, started_at: null, last_complete_at: null, next_due_at: null } },
  points: FEEDER_STATE.points.map((p) => ({ ...p, asdu: null, value: null, quality: { ...p.quality, iv: true, nt: true }, stale_reason: "never_received", cot: null, received_at: null, source_time: null, source_time_invalid: null })),
};
export const CONNECT_GI_STATE: OperatorState = {
  ...FEEDER_STATE,
  points: FEEDER_STATE.points.map((p) => ({ ...p, asdu: p.kind === "float" ? "M_ME_NC_1" : p.kind === "single-point" ? "M_SP_NA_1" : p.kind === "double-point" ? "M_DP_NA_1" : "M_ST_NA_1", cot: 20, received_at: "2026-10-09T11:59:58.200Z", source_time: null, source_time_invalid: null })),
};
export const BREAKER_EVENT_STATE: OperatorState = {
  ...CONNECT_GI_STATE,
  points: CONNECT_GI_STATE.points.map((p) => p.id === "feeder-breaker-position" ? { ...p, value: "open", asdu: "M_DP_TB_1", cot: 3, source_time: "2026-10-09T12:00:00.120Z", source_time_invalid: false, received_at: "2026-10-09T12:00:00.150Z" } : p),
};
export const PERIODIC_GI_STATE: OperatorState = {
  ...BREAKER_EVENT_STATE,
  link: { ...BREAKER_EVENT_STATE.link, gi: { ...BREAKER_EVENT_STATE.link.gi, state: "running", trigger: "periodic", started_at: "2026-10-09T12:14:58.100Z", next_due_at: "2026-10-09T12:29:58.100Z" } },
};
export const LOST_LINK_STATE: OperatorState = {
  ...BREAKER_EVENT_STATE,
  link: { ...BREAKER_EVENT_STATE.link, state: "disconnected", gi: { state: "none", trigger: null, started_at: null, last_complete_at: null, next_due_at: null } },
  points: BREAKER_EVENT_STATE.points.map((p) => ({ ...p, quality: { ...p.quality, iv: true, nt: true }, stale_reason: "link_lost" })),
};
export const WATCHDOG_STATE: OperatorState = {
  ...BREAKER_EVENT_STATE,
  points: BREAKER_EVENT_STATE.points.map((p) => p.id === "oltc-1-bus-kv" ? { ...p, quality: { ...p.quality, nt: true }, stale_reason: "not_updated" } : p),
};
export const WORKED_STATES = [BOOT_STATE, CONNECT_GI_STATE, BREAKER_EVENT_STATE, PERIODIC_GI_STATE, LOST_LINK_STATE, WATCHDOG_STATE];
