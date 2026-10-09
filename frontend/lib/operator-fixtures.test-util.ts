// Answers of the EU control-centre, instructor and firewall-session
// routes in the shapes the backend and the services really send
// (services/iec104/src/cc_*.c, services/devices, backend/internal/server),
// shared by the operator tests.

import type { PackageSummary } from "./api";
import type {
  CommandResult,
  DeviceAudit,
  DeviceState,
  FirewallSessionList,
  InstructorState,
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
  schema: 1,
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

export function commandResult(status: CommandResult["status"]): CommandResult {
  const base: CommandResult = {
    id: "cmd-17",
    status,
    select: { status: "confirmed", at: "2026-10-09T12:00:01.000Z" },
    execute: { status: "confirmed", at: "2026-10-09T12:00:01.100Z", terminated_at: "2026-10-09T12:00:01.700Z" },
    feedback: { status: "matched", value: "open", received_at: "2026-10-09T12:00:01.600Z" },
    error: null,
  };
  switch (status) {
    case "completed":
      return base;
    case "rejected":
      return {
        ...base,
        execute: { status: "negative", at: "2026-10-09T12:00:01.100Z", terminated_at: null },
        feedback: { status: "missing", value: null, received_at: null },
        error: "execute rejected (negative ACT-CON)",
      };
    case "failed":
      return {
        ...base,
        execute: { status: null, at: null, terminated_at: null },
        feedback: { status: "missing", value: null, received_at: null },
        error: "link lost before the execute was sent",
      };
    case "unknown":
      return {
        ...base,
        execute: { status: "timeout", at: "2026-10-09T12:00:01.100Z", terminated_at: null },
        feedback: { status: "missing", value: null, received_at: null },
        error: "no execute confirmation within the command budget",
      };
  }
}

export const TRUTH: InstructorState = {
  device: {
    schema: 1,
    device_id: "feeder-breaker",
    generation: 3141592,
    revision: 5,
    position: "open",
    remote_enabled: true,
    lockout: true,
    fault_active: true,
    protection_tripped: true,
    event_seq: 9,
    changed_at: "2026-10-09T12:00:12.000Z",
    measurements: {
      current_a: 0.0,
      voltage_kv_ll: 0.0,
      valid: true,
      solver_epoch: "e1",
      solve_seq: 140,
      solved_at: "2026-10-09T12:00:29.500Z",
      received_at: "2026-10-09T12:00:29.510Z",
    },
  },
  process: {
    schema: 1,
    profile_id: "eu-feeder-20kv",
    frequency_hz: 50,
    primary_kv: 20,
    breaker: "open",
    source_voltage_kv_ll: 20.0,
    load_voltage_kv_ll: 0.0,
    current_a: 0.0,
    load_kw: 0,
    energized: false,
    converged: true,
    solver_epoch: "e1",
    solve_seq: 140,
    solved_at: "2026-10-09T12:00:29.500Z",
    truth_revision: 5,
    truth_generation: 3141592,
    truth_stale: false,
  },
};

const CLOSED_DEVICE: DeviceState = {
  ...TRUTH.device,
  revision: 4,
  position: "closed",
  lockout: false,
  fault_active: false,
  protection_tripped: false,
  event_seq: 8,
  changed_at: "2026-10-09T11:50:00.000Z",
};

const INSTRUCTOR = { peer: "10.99.99.3:51234", protocol: "instructor", zone: "mgmt" };

export const AUDIT: DeviceAudit = {
  generation: 3141592,
  oldest_seq: 1,
  latest_seq: 4,
  entries: [
    {
      seq: 1,
      at: "2026-10-09T12:00:12.000Z",
      operation: "fault",
      result: "accepted",
      before: CLOSED_DEVICE,
      after: TRUTH.device,
      context: INSTRUCTOR,
    },
    {
      seq: 2,
      at: "2026-10-09T12:00:12.000Z",
      operation: "local_protection_trip",
      result: "accepted",
      before: CLOSED_DEVICE,
      after: TRUTH.device,
      context: { peer: "protection", protocol: "local-protection", zone: "field" },
      detail: { cause: "fault_active" },
    },
    {
      seq: 3,
      at: "2026-10-09T12:00:12.900Z",
      operation: "first_solve",
      result: "accepted",
      before: TRUTH.device,
      after: TRUTH.device,
      context: { peer: "10.50.50.20:40112", protocol: "process", zone: "process" },
      detail: { revision: 5, solver_epoch: "e1", solve_seq: 113, solved_at: "2026-10-09T12:00:12.800Z", current_a: 0 },
    },
    {
      seq: 4,
      at: "2026-10-09T12:00:13.500Z",
      operation: "close",
      result: "rejected",
      before: TRUTH.device,
      after: TRUTH.device,
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
