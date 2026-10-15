// Typed clients for the EU control-centre, instructor and firewall
// session routes. Shapes follow the increment-3 contract: the control
// centre (CC) state, commands and journal (§6) as the backend wraps
// them (§7.2), the feeder-breaker device state and audit (§4) and the
// solver output (§5). Timestamps are RFC3339 strings; null means "not
// known", never a default.

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
  // CC's ephemeral port (lib/firewall-sessions.ts).
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

export type StaleReason = "link_lost" | "device_unreachable" | "measurement_invalid" | "never_received";
export type DoublePointValue = "intermediate" | "open" | "closed" | "indeterminate";
// Double points carry their state name, single points a bool, floats a
// number; null only when the CC has never received the point.
export type PointValue = DoublePointValue | boolean | number | null;

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
  schema: number;
  cache_epoch: string;
  link: ControlCentreLink;
  points: OperatorPoint[];
  package_id: string;
  range_generation: number;
};

export type CommandOperation = "open" | "close";
export type CommandStatus = "completed" | "rejected" | "failed" | "unknown";
export type CommandPhaseStatus = "confirmed" | "negative" | "timeout" | null;
export type FeedbackStatus = "matched" | "mismatched" | "missing";

// id is the CC's "cmd-<n>", counted per CC boot.
export type CommandResult = {
  id: string;
  status: CommandStatus;
  select: { status: CommandPhaseStatus; at: string | null };
  execute: { status: CommandPhaseStatus; at: string | null; terminated_at: string | null };
  // The feedback point is a double point; value is null when missing.
  feedback: { status: FeedbackStatus; value: DoublePointValue | null; received_at: string | null };
  error: string | null;
};

// The one command point of the slice (points.json `commands`).
export const BREAKER_COMMAND_POINT = "feeder-breaker-command";

// Synchronous: the CC answers after the whole select/execute/feedback
// sequence. 400/409/503 from the CC arrive as an ApiError.
export async function sendOperatorCommand(operation: CommandOperation) {
  return request<CommandResult>("/operator/commands", {
    method: "POST",
    body: JSON.stringify({ point: BREAKER_COMMAND_POINT, operation }),
  });
}

export async function getOperatorState() {
  return request<OperatorState>("/operator/state");
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

// ── Instructor truth (GET /api/instructor/*) ─────────────────────

export type DeviceMeasurements = {
  current_a: number | null;
  voltage_kv_ll: number | null;
  valid: boolean;
  solver_epoch: string | null;
  solve_seq: number | null;
  solved_at: string | null;
  received_at: string | null;
};

export type BreakerPosition = "open" | "closed";

// GET /api/state of the device; fault and reset answer with it too.
export type DeviceState = {
  schema: number;
  device_id: string;
  generation: number;
  revision: number;
  position: BreakerPosition;
  remote_enabled: boolean;
  lockout: boolean;
  fault_active: boolean;
  protection_tripped: boolean;
  event_seq: number;
  changed_at: string;
  measurements: DeviceMeasurements;
};

export type ProcessElectrical = {
  schema: number;
  profile_id: string;
  frequency_hz: number;
  primary_kv: number;
  breaker: BreakerPosition;
  source_voltage_kv_ll: number;
  load_voltage_kv_ll: number;
  current_a: number;
  load_kw: number;
  energized: boolean;
  converged: boolean;
  solver_epoch: string;
  solve_seq: number;
  solved_at: string;
  truth_revision: number;
  truth_generation: number;
  truth_stale: boolean;
};

export type InstructorState = {
  device: DeviceState;
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

export async function getInstructorState() {
  return request<InstructorState>("/instructor/state");
}

export async function getInstructorAudit() {
  return request<DeviceAudit>("/instructor/audit?after=0");
}

export async function setInstructorFault(active: boolean) {
  return request<DeviceState>("/instructor/fault", {
    method: "POST",
    body: JSON.stringify({ active }),
  });
}

export async function resetInstructorDevice() {
  return request<DeviceState>("/instructor/reset", { method: "POST", body: "{}" });
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

// The original-direction tuple containd deletes. Zero ports are not
// wildcards, so a kill always names both.
export type SessionTuple = {
  proto: string;
  src: string;
  dst: string;
  sport: number;
  dport: number;
};

export type SessionFilter = { dport: number; src?: string; dst?: string };

export async function getFirewallSessions(filter: SessionFilter) {
  const query = new URLSearchParams({ dport: String(filter.dport) });
  if (filter.src !== undefined) query.set("src", filter.src);
  if (filter.dst !== undefined) query.set("dst", filter.dst);
  return request<FirewallSessionList>(`/firewall/sessions?${query.toString()}`);
}

// 404 when containd has no such entry.
export async function killFirewallSession(tuple: SessionTuple) {
  return request<{ status: string }>("/firewall/sessions/kill", {
    method: "POST",
    body: JSON.stringify(tuple),
  });
}
