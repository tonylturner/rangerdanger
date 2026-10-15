// EU increment-4 wire contracts. Null is unknown, never a default.
import { request } from "./api";

// ── Control centre (GET /api/operator/*) ─────────────────────────

export type LinkState = "disconnected" | "connecting" | "started";
export type GiState = "none" | "running" | "complete";

export type GeneralInterrogation = {
  state: GiState;
  started_at: string | null;
  completed_at: string | null;
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

// ov is null for status points (it only applies to measurements).
export type PointQuality = {
  iv: boolean;
  nt: boolean;
  sb: boolean;
  bl: boolean;
  ov: boolean | null;
};

export type StaleReason = "link_lost" | "device_unreachable" | "measurement_invalid" | "never_received" | "heartbeat";
export type DoublePointValue = "intermediate" | "open" | "closed" | "indeterminate";
// Double points carry their state name, single points a bool, floats a
// number; null only when the CC has never received the point.
export type StepValue = { value: number; transient: boolean };
export type PointValue = DoublePointValue | boolean | number | StepValue | null;

export type OperatorPoint = {
  id: string;
  ioa: number;
  asdu: string;
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
  schema: 2;
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
  reason: string;
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
  if (!["completed", "rejected", "failed", "unknown"].includes(result.result) || typeof result.reason !== "string") {
    throw new Error("Expected increment-4 command result; command outcome is unknown");
  }
  return result;
}

export async function getOperatorState() {
  const state = await request<OperatorState>("/operator/state");
  if (state.schema !== 2) throw new Error("Expected CC state schema 2");
  return state;
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
  fault_current: boolean | null;
  solver_epoch: string | null;
  solve_seq: number | null;
  solved_at: string | null;
  received_at: string | null;
};

export type BreakerPosition = "open" | "closed";
type DeviceCommon = {
  schema: 2;
  device_id: string;
  generation: number;
  revision: number;
  event_seq: number;
  changed_at: string;
  remote_enabled: boolean;
  measurements: DeviceMeasurements;
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
  schema: 2;
  profile_id: string;
  solver_epoch: string;
  solve_seq: number;
  solved_at: string;
  truth_stale: boolean;
  fault_section: "none" | "S1" | "S2";
  f2_tripped: boolean;
  load_multiplier: number;
  source_pu: number;
  tap: number;
  buses: Record<string, { kv: number | null; live: boolean | null }>;
  devices: Record<string, { delivered: boolean; solved_revision: number; revision: number }>;
};

export type FieldState = {
  devices: Record<string, DeviceState>;
  process: ProcessElectrical;
};

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
  if (state.process.schema !== 2 || !state.devices || Object.values(state.devices).some((d) => d.schema !== 2)) {
    throw new Error("Expected field and process schema 2");
  }
  return state;
}

export async function getFieldAudit() {
  return request<DeviceAudit>("/field/audit?after=0");
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
