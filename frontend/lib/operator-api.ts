// EU increment-4 wire contracts. Null is unknown, never a default.
import { request } from "./api";

// ── Control centre (GET /api/operator/*) ─────────────────────────

export type LinkState = "disconnected" | "connecting" | "started";
export type GiState = "none" | "running" | "complete";

export type GeneralInterrogation = {
  state: GiState;
  started_at: string | null;
  trigger: "connect" | "periodic" | "operator" | null;
  last_complete_at: string | null;
  next_due_at: string | null;
};

export type ControlCentreLink = {
  state: LinkState;
  // The RTU, "<ip>:<port>". lib60870 cannot report the CC's own end of
  // the session, so the firewall session list is the only source of the
  // CC's ephemeral port.
  peer: string;
  since: string | null;
  last_rx_at: string | null;
  connection_seq: number;
  gi: GeneralInterrogation;
};

// Float measurements and step positions carry QDS, including boolean OV.
// Single/double indications do not carry OV and represent it as null.
export type PointQuality = {
  iv: boolean;
  nt: boolean;
  sb: boolean;
  bl: boolean;
  ov: boolean | null;
};

export type IndicationQuality = PointQuality & { ov: null };
export type QdsQuality = PointQuality & { ov: boolean };

export type StaleReason = "link_lost" | "device_unreachable" | "measurement_invalid" | "never_received" | "heartbeat" | "not_updated";
export type DoublePointValue = "intermediate" | "open" | "closed" | "indeterminate";
// Double points carry their state name, single points a bool, floats a
// number; null only when the CC has never received the point.
export type StepValue = { value: number; transient: boolean };
export type PointValue = DoublePointValue | boolean | number | StepValue | null;

export type PointKind = "single-point" | "double-point" | "step-position" | "float";
export type Reporting = { cyclic_ms: number; deadband: number };

export type OperatorPoint = {
  id: string;
  device: string;
  limits: { low: number; high: number } | null;
  ioa: number;
  kind: PointKind;
  asdu: string | null;
  reporting: Reporting | null;
  value: PointValue;
  quality: PointQuality;
  stale_reason: StaleReason | null;
  // RTU observation time (CP56Time2a) of the last report. null for
  // untimed ASDUs: always for the floats (M_ME_NC_1), and for status
  // points answered by GI until their next timed report.
  source_time: string | null;
  source_time_invalid: boolean | null;
  received_at: string | null;
  cot: number | null;
};

export type OperatorState = {
  schema: 3;
  snapshot_seq: number;
  rendered_at: string;
  cache_epoch: string;
  link: ControlCentreLink;
  points: OperatorPoint[];
  package_id: string;
  range_generation: number;
};

export type CommandOperation = "open" | "close" | "lower" | "raise" | "auto" | "manual";
export type CommandStatus = "completed" | "rejected" | "failed" | "unknown";
export type CommandResult = {
  id: string;
  point: string;
  operation: CommandOperation;
  result: CommandStatus;
  reason: string | null;
  selected_at: string | null;
  executed_at: string | null;
  con_at: string | null;
  term_at: string | null;
  feedback: { ioa: number; value: PointValue; at: string } | null;
};

export type OperatorCommand = { point: string; operation: CommandOperation };

// One request performs wire select/execute. Never retry an ambiguous write.
export async function sendOperatorCommand(command: OperatorCommand) {
  const result = await request<CommandResult>("/operator/commands", {
    method: "POST", body: JSON.stringify(command),
  });
  if (!["completed", "rejected", "failed", "unknown"].includes(result.result) || (result.reason !== null && typeof result.reason !== "string")) {
    throw new Error("Expected increment-4 command result; command outcome is unknown");
  }
  return result;
}

export class OperatorStateSchemaError extends Error {
  constructor() {
    super("Expected CC state schema 3 with valid IEC reporting and GI");
    this.name = "OperatorStateSchemaError";
  }
}

export async function getOperatorState() {
  const state = await request<unknown>("/operator/state");
  if (!validOperatorState(state)) throw new OperatorStateSchemaError();
  return state;
}

// A request only queues GI. It does not claim ACT-TERM or integrity.
export async function postOperatorGi() {
  const answer = await request<{ trigger: "operator"; requested_at: string }>("/operator/gi", { method: "POST" });
  if (!isObject(answer) || answer.trigger !== "operator" || !timestamp(answer.requested_at)) {
    throw new Error("Expected accepted operator interrogation");
  }
  return answer;
}

// Why the CC journals a link_down. lib60870 reports no close cause, so
// the CC infers it (services/iec104/README.md).
export type LinkDownReason = "t1_timeout" | "peer_closed" | "connect_failed" | "io_error";

// detail differs per kind (link_up, link_down, gi_started, gi_complete,
// point_stale, point_good, command); lib/operator-format.ts reads it.
export type JournalEntry = {
  seq: number;
  at: string;
  kind: string;
  detail: Record<string, unknown>;
};

// The sequence bounds are null while the journal is empty.
export type OperatorJournal = {
  cache_epoch: string;
  entries: JournalEntry[];
  latest_seq: number | null;
  oldest_seq: number | null;
};

export async function getOperatorJournal() {
  return request<OperatorJournal>("/operator/journal?after=0");
}

// ── Physical feeder state (GET /api/field/*) ─────────────────────

export type DeviceMeasurements = {
  valid: boolean;
  current_a: number | null;
  voltage_kv_ll: number | null;
  source_kv_ll: number | null;
  section_live: boolean | null;
  source_live: boolean | null;
  p_mw: number | null;
  q_mvar: number | null;
  pf: number | null;
  solver_epoch: string | null;
  solve_seq: number | null;
  solved_at: string | null;
  received_at: string | null;
};

export type Protection = {
  function: "overcurrent" | "fpi" | "oltc_block";
  pickup_a: number;
  delay_ms: number;
  instantaneous_a: number | null;
  picked_up: boolean;
  picked_up_since: string | null;
  last_trip_current_a: number | null;
};
export type FieldDeviceKey = "breaker-1" | "rmu-1" | "rmu-2" | "oltc-1";
export type BreakerPosition = "open" | "closed";
type DeviceCommon = {
  schema: 3;
  device_id: string;
  generation: number;
  revision: number;
  event_seq: number;
  changed_at: string;
  remote_enabled: boolean;
  measurements: DeviceMeasurements;
  protection: Protection;
};
export type BreakerState = DeviceCommon & {
  kind: "breaker";
  position: BreakerPosition;
  lockout: boolean;
  protection_tripped: boolean;
};
export type RmuState = DeviceCommon & {
  kind: "rmu";
  position: BreakerPosition;
  earth_closed: boolean;
  fault_passage: boolean;
};
export type OltcState = DeviceCommon & {
  kind: "oltc";
  tap: number;
  target_tap: number;
  moving: boolean;
  auto: boolean;
  blocked: boolean;
  armed: boolean;
  plc_request_result: string;
  heartbeat: number;
};
export type DeviceState = BreakerState | RmuState | OltcState;

export type ProcessElectrical = {
  schema: 3;
  profile_id: string;
  solver_epoch: string;
  solve_seq: number;
  solved_at: string | null;
  truth_stale: boolean;
  fault_section: "none" | "S1" | "S2";
  f2_tripped: boolean;
  load_multiplier: number;
  source_pu: number;
  tap: number;
  buses: Record<string, { kv: number | null; live: boolean | null }>;
  profile: { enabled: boolean; load_factors: { residential: number | null; commercial: number | null; industrial: number | null }; source_pu: number | null };
  f2_protection: Pick<Protection, "picked_up" | "picked_up_since" | "last_trip_current_a">;
  devices: Record<string, { delivered: boolean; solved_revision: number | null; revision: number | null; current_a: number | null; p_mw: number | null; q_mvar: number | null }>;
};

export type FieldState = {
  devices: Record<string, DeviceState>;
  // No time-source sim declared: null. A failed/malformed clock is an error.
  clock: StationClock | null;
  process: ProcessElectrical;
};

// GPS /api/state is not a schema-3 feeder device.
export type StationClock = {
  actual_time: string;
  reported_time: string;
  sync_status: "locked" | "holdover" | "freerun";
  time_offset_sec: number;
  satellite_count: number;
  holdover_started: string;
  holdover_drift_ppm: number;
  ntp_enabled: boolean;
  ptp_enabled: boolean;
  irig_b_output: boolean;
  alarm: boolean;
  comms_ok: boolean;
  last_command_source: string;
};

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const finite = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value);
const integer = (value: unknown): value is number => finite(value) && Number.isInteger(value);
const timestamp = (value: unknown) => typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) && Number.isFinite(Date.parse(value));
const nullable = (value: unknown, valid: (v: unknown) => boolean) => value === null || valid(value);
const booleans = (value: Record<string, unknown>, keys: string[]) => keys.every((key) => typeof value[key] === "boolean");
const strings = (value: Record<string, unknown>, keys: string[]) => keys.every((key) => typeof value[key] === "string");

const exactKeys = (o: Record<string, unknown>, keys: string[]) =>
  Object.keys(o).length === keys.length && keys.every((k) => Object.hasOwn(o, k));
const ASDU_BY_KIND: Record<PointKind, string[]> = {
  "single-point": ["M_SP_NA_1", "M_SP_TB_1"],
  "double-point": ["M_DP_NA_1", "M_DP_TB_1"],
  "step-position": ["M_ST_NA_1", "M_ST_TB_1"],
  float: ["M_ME_NC_1"],
};
function validPoint(p: unknown): boolean {
  if (!isObject(p) || !exactKeys(p, ["id", "device", "limits", "ioa", "kind", "asdu", "value", "quality", "stale_reason", "source_time", "source_time_invalid", "received_at", "cot", "reporting"]) ||
    typeof p.id !== "string" || typeof p.device !== "string" || p.device.length === 0 ||
    !nullable(p.limits, (l) => isObject(l) && exactKeys(l, ["low", "high"]) && finite(l.low) && finite(l.high) && l.low < l.high) || !integer(p.ioa) || p.ioa < 1 || p.ioa > 16777215 ||
    typeof p.kind !== "string" || !Object.hasOwn(ASDU_BY_KIND, p.kind)) return false;
  const kind = p.kind as PointKind;
  if (!nullable(p.asdu, (v) => typeof v === "string" && ASDU_BY_KIND[kind].includes(v)) ||
    !["source_time", "received_at"].every((k) => nullable(p[k], timestamp)) ||
    !nullable(p.source_time_invalid, (v) => typeof v === "boolean") ||
    !nullable(p.cot, (v) => integer(v) && v >= 0 && v <= 63) ||
    !nullable(p.stale_reason, (v) => typeof v === "string" && ["never_received", "link_lost", "device_unreachable", "heartbeat", "measurement_invalid", "not_updated"].includes(v))) return false;
  const q = p.quality;
  if (!isObject(q) || !exactKeys(q, ["iv", "nt", "sb", "bl", "ov"]) || !booleans(q, ["iv", "nt", "sb", "bl"]) ||
    ((kind === "float" || kind === "step-position") ? typeof q.ov !== "boolean" : q.ov !== null)) return false;
  const r = p.reporting;
  if (kind === "float") {
    if (!isObject(r) || !exactKeys(r, ["cyclic_ms", "deadband"]) || !integer(r.cyclic_ms) || r.cyclic_ms < 1000 || r.cyclic_ms > 3600000 || !finite(r.deadband) || r.deadband < 0) return false;
  } else if (r !== null) return false;
  if (p.value === null) return true;
  switch (kind) {
    case "single-point": return typeof p.value === "boolean";
    case "double-point": return typeof p.value === "string" && ["intermediate", "open", "closed", "indeterminate"].includes(p.value);
    case "float": return finite(p.value);
    case "step-position": return isObject(p.value) && exactKeys(p.value, ["value", "transient"]) && integer(p.value.value) && p.value.value >= -64 && p.value.value <= 63 && typeof p.value.transient === "boolean";
  }
}
export function validOperatorState(s: unknown): s is OperatorState {
  if (!isObject(s) || !exactKeys(s, ["schema", "snapshot_seq", "rendered_at", "cache_epoch", "link", "points", "package_id", "range_generation"]) ||
    s.schema !== 3 || !integer(s.snapshot_seq) || s.snapshot_seq < 0 || !timestamp(s.rendered_at) ||
    !strings(s, ["cache_epoch", "package_id"]) || !integer(s.range_generation) || s.range_generation < 0 ||
    !Array.isArray(s.points) || !s.points.every(validPoint)) return false;
  const l = s.link;
  if (!isObject(l) || !exactKeys(l, ["state", "peer", "since", "last_rx_at", "connection_seq", "gi"]) ||
    typeof l.state !== "string" || !["disconnected", "connecting", "started"].includes(l.state) || typeof l.peer !== "string" ||
    !integer(l.connection_seq) || l.connection_seq < 0 || !["since", "last_rx_at"].every((k) => nullable(l[k], timestamp))) return false;
  const g = l.gi;
  return isObject(g) && exactKeys(g, ["state", "trigger", "started_at", "last_complete_at", "next_due_at"]) &&
    typeof g.state === "string" && ["none", "running", "complete"].includes(g.state) &&
    nullable(g.trigger, (v) => typeof v === "string" && ["connect", "periodic", "operator"].includes(v)) &&
    ["started_at", "last_complete_at", "next_due_at"].every((k) => nullable(g[k], timestamp));
}

function validDevice(value: unknown): boolean {
  if (!isObject(value) || value.schema !== 3 || !strings(value, ["device_id"]) ||
    !["generation", "revision", "event_seq"].every((key) => integer(value[key]) && value[key] >= 0) ||
    !timestamp(value.changed_at) || !booleans(value, ["remote_enabled"])) return false;
  const protection = value.protection;
  const functions = { breaker: "overcurrent", rmu: "fpi", oltc: "oltc_block" };
  if (!isObject(protection) || !exactKeys(protection, ["function", "pickup_a", "delay_ms", "instantaneous_a", "picked_up", "picked_up_since", "last_trip_current_a"]) ||
    protection.function !== functions[value.kind as keyof typeof functions] ||
    !finite(protection.pickup_a) || protection.pickup_a <= 0 || !integer(protection.delay_ms) || protection.delay_ms < 0 ||
    !nullable(protection.instantaneous_a, finite) || !validPickup(protection)) return false;
  const m = value.measurements;
  if (!isObject(m) || !exactKeys(m, ["valid", "current_a", "voltage_kv_ll", "source_kv_ll", "section_live", "source_live", "p_mw", "q_mvar", "pf", "solver_epoch", "solve_seq", "solved_at", "received_at"]) || !booleans(m, ["valid"]) ||
    !["current_a", "voltage_kv_ll", "source_kv_ll", "p_mw", "q_mvar", "pf"].every((key) => nullable(m[key], finite)) ||
    !["section_live", "source_live"].every((key) => nullable(m[key], (v) => typeof v === "boolean")) ||
    (m.valid === false && [m.p_mw, m.q_mvar, m.pf].some((v) => v !== null)) ||
    (m.pf !== null && (!finite(m.pf) || m.pf < 0 || m.pf > 1)) ||
    !nullable(m.solver_epoch, (v) => typeof v === "string") || !nullable(m.solve_seq, integer) ||
    !["solved_at", "received_at"].every((key) => nullable(m[key], timestamp))) return false;
  switch (value.kind) {
    case "breaker":
      return (value.position === "open" || value.position === "closed") && booleans(value, ["lockout", "protection_tripped"]);
    case "rmu":
      return (value.position === "open" || value.position === "closed") && booleans(value, ["earth_closed", "fault_passage"]);
    case "oltc":
      return ["tap", "target_tap", "heartbeat"].every((key) => integer(value[key])) &&
        booleans(value, ["moving", "auto", "blocked", "armed"]) && strings(value, ["plc_request_result"]);
    default:
      return false;
  }
}

function validPickup(p: Record<string, unknown>): boolean {
  return typeof p.picked_up === "boolean" && nullable(p.picked_up_since, timestamp) && nullable(p.last_trip_current_a, finite);
}
function validProcess(p: unknown): boolean {
  if (!isObject(p) || p.schema !== 3 || !strings(p, ["profile_id", "solver_epoch"]) || !integer(p.solve_seq) || p.solve_seq < 0 ||
    !nullable(p.solved_at, timestamp) || !booleans(p, ["truth_stale", "f2_tripped"]) ||
    (typeof p.fault_section !== "string" || !["none", "S1", "S2"].includes(p.fault_section)) ||
    !["load_multiplier", "source_pu", "tap"].every((k) => finite(p[k]))) return false;
  const profile = p.profile, f2 = p.f2_protection;
  if (!isObject(profile) || !exactKeys(profile, ["enabled", "load_factors", "source_pu"]) || typeof profile.enabled !== "boolean" ||
    !nullable(profile.source_pu, finite) || !isObject(profile.load_factors) ||
    !exactKeys(profile.load_factors, ["residential", "commercial", "industrial"]) || !Object.values(profile.load_factors).every((v) => nullable(v, finite)) ||
    !isObject(f2) || !exactKeys(f2, ["picked_up", "picked_up_since", "last_trip_current_a"]) || !validPickup(f2)) return false;
  return isObject(p.buses) && Object.values(p.buses).every((b) => isObject(b) && nullable(b.kv, finite) && nullable(b.live, (v) => typeof v === "boolean")) &&
    isObject(p.devices) && Object.values(p.devices).every((d) => isObject(d) && exactKeys(d, ["delivered", "solved_revision", "revision", "current_a", "p_mw", "q_mvar"]) && typeof d.delivered === "boolean" &&
      ["solved_revision", "revision"].every((k) => nullable(d[k], integer)) && ["current_a", "p_mw", "q_mvar"].every((k) => nullable(d[k], finite)));
}

function validClock(value: unknown): boolean {
  return isObject(value) && timestamp(value.actual_time) && timestamp(value.reported_time) &&
    ["locked", "holdover", "freerun"].some((status) => status === value.sync_status) &&
    finite(value.time_offset_sec) && integer(value.satellite_count) && value.satellite_count >= 0 &&
    (value.holdover_started === "" || timestamp(value.holdover_started)) && finite(value.holdover_drift_ppm) &&
    booleans(value, ["ntp_enabled", "ptp_enabled", "irig_b_output", "alarm", "comms_ok"]) &&
    strings(value, ["last_command_source"]);
}

// before and after are full device snapshots. detail is absent, a
// reason string ("no_op", "lockout", "stale_truth", ...) or an object
// (first_solve: revision, solver_epoch, solve_seq, solved_at, current_a).
export type DeviceAuditEntry = {
  seq: number;
  at: string;
  operation: string;
  result: string;
  before: DeviceState;
  after: DeviceState;
  context: { peer: string; protocol: string; zone: string };
  detail?: string | Record<string, unknown>;
};

// The sequence bounds are null while the audit is empty.
export type DeviceAudit = {
  generation: number;
  entries: DeviceAuditEntry[];
  latest_seq: number | null;
  oldest_seq: number | null;
};

export async function getFieldState() {
  const state = await request<FieldState>("/field/state");
  if (!isObject(state) || !validProcess(state.process) || !isObject(state.devices) ||
    Object.keys(state.devices).length === 0 || !Object.values(state.devices).every(validDevice)) {
    throw new Error("Expected field and process schema 3");
  }
  if (state.clock !== null && !validClock(state.clock)) throw new Error("Expected station clock state or null");
  return state;
}

export async function getFieldAudit(device: FieldDeviceKey = "breaker-1") {
  const audit = await request<DeviceAudit>(`/field/audit?after=0&device=${device}`);
  if (!isObject(audit) || !integer(audit.generation) || !Array.isArray(audit.entries) ||
    !(["latest_seq", "oldest_seq"] as const).every((k) => nullable(audit[k], integer)) ||
    !audit.entries.every((e) => isObject(e) && integer(e.seq) && timestamp(e.at) && strings(e, ["operation", "result"]) && validDevice(e.before) && validDevice(e.after) && isObject(e.context) && strings(e.context, ["peer", "protocol", "zone"]))) {
    throw new Error("Expected device audit with schema 3 snapshots");
  }
  return audit;
}

// ── Firewall sessions (GET /api/firewall/sessions) ───────────────

// One containd conntrack entry, original direction. A port or state
// containd left out is null; timeout is in seconds.
export type FirewallSession = {
  proto: string;
  src: string;
  dst: string;
  sport: number | null;
  dport: number | null;
  state: string | null;
  timeout: number;
};

export type FirewallSessionList = {
  sessions: FirewallSession[];
  count: number;
};
