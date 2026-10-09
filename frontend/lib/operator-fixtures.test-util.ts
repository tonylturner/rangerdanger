// Contract-shaped answers of the EU control-centre and instructor
// routes, shared by the operator tests.

import type { PackageSummary } from "./api";
import type {
  CommandResult,
  DeviceAudit,
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
};

export const EU_PACKAGE: PackageSummary = {
  id: "eu-iec104-substation",
  title: "EU IEC 104 Substation",
  revision: 1,
  active: true,
  operator_view: "eu-iec104",
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
    cot: 20,
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
    local: "10.60.60.20:49152",
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
    local: null,
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
    { seq: 1, at: "2026-10-09T11:59:58.000Z", kind: "link_up", detail: { peer: "10.30.30.20:2404", local: "10.60.60.20:49152", connection_seq: 3 } },
    { seq: 2, at: "2026-10-09T11:59:58.300Z", kind: "gi_complete", detail: { connection_seq: 3, points_good: 4 } },
    { seq: 3, at: "2026-10-09T12:00:10.000Z", kind: "link_down", detail: { peer: "10.30.30.20:2404", local: "10.60.60.20:49152", connection_seq: 3, reason: "t1_timeout" } },
    { seq: 4, at: "2026-10-09T12:00:10.000Z", kind: "point_stale", detail: { point: "feeder-breaker-position", stale_reason: "link_lost", value: "closed" } },
  ],
};

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
        error: "negative ACT-CON (interlock)",
      };
    case "failed":
      return {
        ...base,
        execute: { status: null, at: null, terminated_at: null },
        feedback: { status: "missing", value: null, received_at: null },
        error: "link lost before execute",
      };
    case "unknown":
      return {
        ...base,
        execute: { status: "timeout", at: "2026-10-09T12:00:01.100Z", terminated_at: null },
        feedback: { status: "missing", value: null, received_at: null },
        error: "no ACT-CON for execute",
      };
  }
}

export const TRUTH: InstructorState = {
  device: {
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

export const AUDIT: DeviceAudit = {
  generation: 3141592,
  oldest_seq: 1,
  latest_seq: 2,
  entries: [
    {
      seq: 1,
      at: "2026-10-09T12:00:12.000Z",
      operation: "trip",
      result: "accepted",
      before: { position: "closed" },
      after: { position: "open" },
      context: { peer: "local", protocol: "local-protection", zone: "local" },
    },
    {
      seq: 2,
      at: "2026-10-09T12:00:12.900Z",
      operation: "first_solve",
      result: "accepted",
      before: null,
      after: null,
      context: { peer: "10.50.50.20", protocol: "process", zone: "process" },
      detail: { revision: 5, solver_epoch: "e1", solve_seq: 113, solved_at: "2026-10-09T12:00:12.800Z", current_a: 0 },
    },
  ],
};
